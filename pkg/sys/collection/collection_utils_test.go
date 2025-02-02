/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
*
* @author Michael Saigachenko
*/

package collection

import (
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	queryprocessor "github.com/voedger/voedger/pkg/processors/query"
)

type testDataType struct {
	appQName      istructs.AppQName
	appPartsCount int
	appEngines    [cluster.ProcessorKind_Count]int

	pkgName string

	// common event entites
	partitionIdent string
	partition      istructs.PartitionID
	workspace      istructs.WSID
	plogStartOfs   istructs.Offset

	// function
	modifyCmdName       appdef.QName
	modifyCmdParamsName appdef.QName
	modifyCmdResultName appdef.QName

	// records
	tableArticles      appdef.QName
	articleNameIdent   string
	articleNumberIdent string
	articleDeptIdent   string

	tableArticlePrices        appdef.QName
	articlePricesPriceIdent   string
	articlePricesPriceIdIdent string

	tableArticlePriceExceptions         appdef.QName
	articlePriceExceptionsPeriodIdIdent string
	articlePriceExceptionsPriceIdent    string

	tableDepartments appdef.QName
	depNameIdent     string
	depNumberIdent   string

	tablePrices      appdef.QName
	priceNameIdent   string
	priceNumberIdent string

	tablePeriods      appdef.QName
	periodNameIdent   string
	periodNumberIdent string

	// backoffice
	cocaColaNumber  int32
	cocaColaNumber2 int32
	fantaNumber     int32
}

const OccursUnbounded = appdef.Occurs(0xffff)

var test = testDataType{
	appQName:      istructs.AppQName_test1_app1,
	appPartsCount: 64,
	appEngines:    cluster.PoolSize(100, 100, 100),

	pkgName: "test",

	partitionIdent:      "Partition",
	partition:           55,
	workspace:           1234,
	plogStartOfs:        1,
	modifyCmdName:       appdef.NewQName("test", "modify"),
	modifyCmdParamsName: appdef.NewQName("test", "modifyArgs"),
	modifyCmdResultName: appdef.NewQName("test", "modifyResult"),

	/////
	tableArticles:      appdef.NewQName("test", "articles"),
	articleNameIdent:   "name",
	articleNumberIdent: "number",
	articleDeptIdent:   "id_department",

	tableArticlePrices:        appdef.NewQName("test", "article_prices"),
	articlePricesPriceIdIdent: "id_prices",
	articlePricesPriceIdent:   "price",

	tableArticlePriceExceptions:         appdef.NewQName("test", "article_price_exceptions"),
	articlePriceExceptionsPeriodIdIdent: "id_periods",
	articlePriceExceptionsPriceIdent:    "price",

	tableDepartments: appdef.NewQName("test", "departments"),
	depNameIdent:     "name",
	depNumberIdent:   "number",

	tablePrices:      appdef.NewQName("test", "prices"),
	priceNameIdent:   "name",
	priceNumberIdent: "number",

	tablePeriods:      appdef.NewQName("test", "periods"),
	periodNameIdent:   "name",
	periodNumberIdent: "number",

	// backoffice
	cocaColaNumber:  10,
	cocaColaNumber2: 11,
	fantaNumber:     12,
}

type idsGeneratorType struct {
	istructs.IIDGenerator
	idmap          map[istructs.RecordID]istructs.RecordID
	nextPlogOffset istructs.Offset
}

func (me *idsGeneratorType) NextID(rawID istructs.RecordID, t appdef.IType) (storageID istructs.RecordID, err error) {
	if storageID, err = me.IIDGenerator.NextID(rawID, t); err != nil {
		return istructs.NullRecordID, err
	}
	me.idmap[rawID] = storageID
	return
}

func (me *idsGeneratorType) nextOffset() (offset istructs.Offset) {
	offset = me.nextPlogOffset
	me.nextPlogOffset++
	return
}

func (me *idsGeneratorType) decOffset() {
	me.nextPlogOffset--
}

func newIdsGenerator() idsGeneratorType {
	return idsGeneratorType{
		idmap:          make(map[istructs.RecordID]istructs.RecordID),
		nextPlogOffset: test.plogStartOfs,
		IIDGenerator:   istructsmem.NewIDGenerator(),
	}
}

func requireArticle(require *require.Assertions, name string, number int32, as istructs.IAppStructs, articleId istructs.RecordID) {
	kb := as.ViewRecords().KeyBuilder(QNameCollectionView)
	kb.PutInt32(Field_PartKey, PartitionKeyCollection)
	kb.PutQName(Field_DocQName, test.tableArticles)
	kb.PutRecordID(field_DocID, articleId)
	kb.PutRecordID(field_ElementID, istructs.NullRecordID)
	value, err := as.ViewRecords().Get(test.workspace, kb)
	require.NoError(err)
	recArticle := value.AsRecord(Field_Record)
	require.Equal(name, recArticle.AsString(test.articleNameIdent))
	require.Equal(number, recArticle.AsInt32(test.articleNumberIdent))
}

func requireArPrice(require *require.Assertions, priceId istructs.RecordID, price float32, as istructs.IAppStructs, articleId, articlePriceId istructs.RecordID) {
	kb := as.ViewRecords().KeyBuilder(QNameCollectionView)
	kb.PutInt32(Field_PartKey, PartitionKeyCollection)
	kb.PutQName(Field_DocQName, test.tableArticles)
	kb.PutRecordID(field_DocID, articleId)
	kb.PutRecordID(field_ElementID, articlePriceId)
	value, err := as.ViewRecords().Get(test.workspace, kb)
	require.NoError(err)
	recArticlePrice := value.AsRecord(Field_Record)
	require.Equal(priceId, recArticlePrice.AsRecordID(test.articlePricesPriceIdIdent))
	require.Equal(price, recArticlePrice.AsFloat32(test.articlePricesPriceIdent))
}

func requireArPriceException(require *require.Assertions, periodId istructs.RecordID, price float32, as istructs.IAppStructs, articleId, articlePriceExceptionId istructs.RecordID) {
	kb := as.ViewRecords().KeyBuilder(QNameCollectionView)
	kb.PutInt32(Field_PartKey, PartitionKeyCollection)
	kb.PutQName(Field_DocQName, test.tableArticles)
	kb.PutRecordID(field_DocID, articleId)
	kb.PutRecordID(field_ElementID, articlePriceExceptionId)
	value, err := as.ViewRecords().Get(test.workspace, kb)
	require.NoError(err)
	recArticlePriceException := value.AsRecord(Field_Record)
	require.Equal(periodId, recArticlePriceException.AsRecordID(test.articlePriceExceptionsPeriodIdIdent))
	require.Equal(price, recArticlePriceException.AsFloat32(test.articlePriceExceptionsPriceIdent))
}

type resultElementRow []interface{}

type resultElement []resultElementRow

type resultRow []resultElement

type testResultSenderClosable struct {
	done       chan interface{}
	resultRows []resultRow
	handledErr error
}

func (s *testResultSenderClosable) StartArraySection(sectionType string, path []string) {
}
func (s *testResultSenderClosable) StartMapSection(string, []string) { panic("implement me") }
func (s *testResultSenderClosable) ObjectSection(sectionType string, path []string, element interface{}) (err error) {
	return nil
}
func (s *testResultSenderClosable) SendElement(name string, sentRow interface{}) (err error) {
	sentElements := sentRow.([]interface{})
	resultRow := make([]resultElement, len(sentElements))

	for elmIndex, sentElement := range sentElements {
		sentElemRows := sentElement.([]queryprocessor.IOutputRow)
		resultElm := make([]resultElementRow, len(sentElemRows))
		for i, sentElemRow := range sentElemRows {
			resultElm[i] = sentElemRow.Values()
		}
		resultRow[elmIndex] = resultElm
	}

	s.resultRows = append(s.resultRows, resultRow)
	return nil
}
func (s *testResultSenderClosable) Close(err error) {
	s.handledErr = err
	close(s.done)
}
func (s *testResultSenderClosable) requireNoError(t *require.Assertions) {
	if s.handledErr != nil {
		t.FailNow(s.handledErr.Error())
	}
}

func newTestSender() *testResultSenderClosable {
	return &testResultSenderClosable{
		done:       make(chan interface{}),
		resultRows: make([]resultRow, 0), // array of elements, each element is array rows,
	}
}
