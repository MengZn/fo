package types

import (
	"fmt"

	"github.com/albrow/fo/ast"
)

// concreteType returns a new type with the concrete type parameters of e
// applied.
func (check *Checker) concreteType(e *ast.Ident, genericObj Object) Type {
	switch genericObj := genericObj.(type) {
	case *TypeName:
		if named, ok := genericObj.typ.(*Named); ok {
			typeMap := check.createTypeMap(e.TypeParams, named.typeParams)
			newNamed := replaceTypesInNamed(named, typeMap)
			newNamed.typeParams = nil
			typeParams := make([]*TypeParam, len(named.typeParams))
			copy(typeParams, named.typeParams)
			newType := NewConcreteNamed(newNamed, typeParams, typeMap)
			newObj := *genericObj
			newType.obj = &newObj
			newObj.typ = newType
			return newType
		}
	case *Func:
		if sig, ok := genericObj.typ.(*Signature); ok {
			// TODO(albrow): Cache concrete types in some sort of special scope so
			// we can avoid re-generating the concrete types on each usage.
			// TODO(albrow): Use a named type/function decl here for better error
			// messages.
			typeMap := check.createTypeMap(e.TypeParams, sig.typeParams)
			newSig := replaceTypesInSignature(sig, typeMap)
			newSig.typeParams = nil
			typeParams := make([]*TypeParam, len(sig.typeParams))
			copy(typeParams, sig.typeParams)
			newType := NewConcreteSignature(newSig, typeParams, typeMap)
			return newType
		}
	}

	check.errorf(check.pos, "unexpected generic type for ident %s: %T", e.Name, genericObj)
	return nil
}

// TODO(albrow): catch case where the wrong number of type parameters has been
// given and test it.
func (check *Checker) createTypeMap(params *ast.TypeParamList, genericParams []*TypeParam) map[string]Type {
	if len(params.List) != len(genericParams) {
		check.errorf(check.pos, "wrong number of type parameters (expected %d but got %d)", len(genericParams), len(params.List))
		return nil
	}
	typeMap := map[string]Type{}
	for i, typ := range params.List {
		var x operand
		check.rawExpr(&x, typ, nil)
		if x.typ != nil {
			typeMap[genericParams[i].String()] = x.typ
		}
	}
	return typeMap
}

// replaceTypes recursively replaces any type parameters starting at root with
// the corresponding concrete type by looking up in typeMapping. typeMapping is
// a map of type parameter identifier to concrete type. replaceTypes works with
// compound types such as maps, slices, and arrays whenever the type parameter
// is part of the type. For example, root can be a []T and replaceTypes will
// correctly replace T with the corresponding concrete type (assuming it is
// included in typeMapping).
func replaceTypes(root Type, typeMapping map[string]Type) Type {
	switch t := root.(type) {
	case *TypeParam:
		if newType, found := typeMapping[t.String()]; found {
			// This part is important; if the concrete type is also a type parameter,
			// don't do the replacement. We assume that we're dealing with an
			// inherited type parameter and that the concrete form of the parent will
			// fill in this missing type parameter in the future. If it is not filled
			// in correctly in the future, we know how to generate an error.
			if _, newIsTypeParam := newType.(*TypeParam); newIsTypeParam {
				return t
			}
			return newType
		}
		// TODO(albrow): handle this error?
		panic(fmt.Errorf("undefined type parameter: %s", t))
	case *Pointer:
		newPointer := *t
		newPointer.base = replaceTypes(t.base, typeMapping)
		return &newPointer
	case *Slice:
		newSlice := *t
		newSlice.elem = replaceTypes(t.elem, typeMapping)
		return &newSlice
	case *Map:
		newMap := *t
		newMap.key = replaceTypes(t.key, typeMapping)
		newMap.elem = replaceTypes(t.elem, typeMapping)
		return &newMap
	case *Array:
		newArray := *t
		newArray.elem = replaceTypes(t.elem, typeMapping)
		return &newArray
	case *Chan:
		newChan := *t
		newChan.elem = replaceTypes(t.elem, typeMapping)
		return &newChan
	case *Struct:
		return replaceTypesInStruct(t, typeMapping)
	case *Signature:
		return replaceTypesInSignature(t, typeMapping)
	case *Named:
		return replaceTypesInNamed(t, typeMapping)
	case *ConcreteNamed:
		return replaceTypesInConcreteNamed(t, typeMapping)
	}
	return root
}

func replaceTypesInStruct(root *Struct, typeMapping map[string]Type) *Struct {
	var fields []*Var
	for _, field := range root.fields {
		newField := *field
		newField.typ = replaceTypes(field.Type(), typeMapping)
		fields = append(fields, &newField)
	}
	return NewStruct(fields, root.tags)
}

func replaceTypesInSignature(root *Signature, typeMapping map[string]Type) *Signature {
	var newRecv *Var
	if root.recv != nil {
		newRecv := *root.recv
		newRecv.typ = replaceTypes(root.recv.typ, typeMapping)
	}

	var newParams *Tuple
	if root.params != nil && len(root.params.vars) > 0 {
		newParams = &Tuple{}
		for _, param := range root.params.vars {
			newParam := *param
			newParam.typ = replaceTypes(param.typ, typeMapping)
			newParams.vars = append(newParams.vars, &newParam)
		}
	}

	var newResults *Tuple
	if root.results != nil && len(root.results.vars) > 0 {
		newResults = &Tuple{}
		for _, result := range root.results.vars {
			newResult := *result
			newResult.typ = replaceTypes(result.typ, typeMapping)
			newResults.vars = append(newResults.vars, &newResult)
		}
	}

	// TODO(albrow): Implement inherited type parameters here?

	return NewSignature(newRecv, newParams, newResults, root.variadic, root.typeParams)
}

func replaceTypesInNamed(root *Named, typeMapping map[string]Type) *Named {
	newUnderlying := replaceTypes(root.underlying, typeMapping)
	newNamed := *root
	newNamed.underlying = newUnderlying
	newObj := *root.obj
	newObj.typ = &newNamed
	newNamed.obj = &newObj
	return &newNamed
}

// TODO(albrow): optimize by doing nothing in the case where the new typeMapping
// is equivalent to the old.
func replaceTypesInConcreteNamed(root *ConcreteNamed, typeMapping map[string]Type) *ConcreteNamed {
	newTypeMap := map[string]Type{}
	for key, given := range root.typeMap {
		if param, givenIsTypeParam := given.(*TypeParam); givenIsTypeParam {
			if inherited, found := typeMapping[param.String()]; found {
				if _, inheritedIsTypeParam := inherited.(*TypeParam); !inheritedIsTypeParam {
					newTypeMap[key] = inherited
					continue
				}
			}
		}
		newTypeMap[key] = given
	}
	newNamed := replaceTypesInNamed(root.Named, newTypeMap)
	newType := NewConcreteNamed(newNamed, root.typeParams, newTypeMap)
	newNamed.typeParams = nil
	newObj := *root.obj
	newObj.typ = newType
	newType.obj = &newObj
	return newType
}
