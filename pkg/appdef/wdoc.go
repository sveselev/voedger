/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

// # Implements:
//   - IWDoc, IWDocBuilder
type wDoc struct {
	doc
}

func newWDoc(app *appDef, name QName) *wDoc {
	d := &wDoc{}
	d.doc = makeDoc(app, name, TypeKind_WDoc, d)
	app.appendType(d)
	return d
}

func (d *wDoc) isWDoc() {}

// # Implements:
//   - IWRecord, IWRecordBuilder
type wRecord struct {
	containedRecord
}

func newWRecord(app *appDef, name QName) *wRecord {
	r := &wRecord{}
	r.containedRecord = makeContainedRecord(app, name, TypeKind_WRecord, r)
	app.appendType(r)
	return r
}

func (r wRecord) isWRecord() {}
