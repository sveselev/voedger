/*
 * Copyright (c) 2023-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

import (
	"fmt"
)

// # Implements:
//   - IExtension
//   - IExtensionBuilder
type extension struct {
	typ
	embeds interface{}
	name   string
	engine ExtensionEngineKind
}

func makeExtension(app *appDef, name QName, kind TypeKind, embeds interface{}) extension {
	e := extension{
		typ:    makeType(app, name, kind),
		embeds: embeds,
		name:   name.Entity(),
		engine: ExtensionEngineKind_BuiltIn,
	}

	return e
}

func (ex extension) Name() string {
	return ex.name
}

func (ex extension) Engine() ExtensionEngineKind {
	return ex.engine
}

func (ex *extension) SetEngine(engine ExtensionEngineKind) IExtensionBuilder {
	if (engine == ExtensionEngineKind_null) || (engine >= ExtensionEngineKind_Count) {
		panic(fmt.Errorf("%v: extension engine kind «%v» is invalid: %w", ex, engine, ErrInvalidExtensionEngineKind))
	}
	ex.engine = engine
	return ex.embeds.(IExtensionBuilder)
}

func (ex *extension) SetName(name string) IExtensionBuilder {
	if name == "" {
		panic(fmt.Errorf("%v: extension name is empty: %w", ex, ErrNameMissed))
	}
	if ok, err := ValidIdent(name); !ok {
		panic(fmt.Errorf("%v: extension name «%s» is not valid: %w", ex, name, err))
	}
	ex.name = name
	return ex.embeds.(IExtensionBuilder)
}

func (ex extension) String() string {
	// BuiltIn-function «test.func»
	return fmt.Sprintf("%s-%v", ex.Engine().TrimString(), ex.typ.String())
}
