/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package queryprocessor

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/iauthnzimpl"
	"github.com/voedger/voedger/pkg/iprocbus"
	"github.com/voedger/voedger/pkg/iratesce"
	"github.com/voedger/voedger/pkg/istorage/mem"
	istorageimpl "github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/itokensjwt"
	imetrics "github.com/voedger/voedger/pkg/metrics"
	"github.com/voedger/voedger/pkg/pipeline"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys/authnz"
	coreutils "github.com/voedger/voedger/pkg/utils"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
)

var now = time.Now()

var timeFunc = coreutils.TimeFunc(func() time.Time { return now })

var (
	appName       istructs.AppQName    = istructs.AppQName_test1_app1
	appPartsCount                      = 1
	appEngines                         = cluster.PoolSize(10, 100, 10)
	partID        istructs.PartitionID = 5
	wsID          istructs.WSID        = 15

	qNameFunction  = appdef.NewQName("bo", "FindArticlesByModificationTimeStampRange")
	qNameQryDenied = appdef.NewQName(appdef.SysPackage, "TestDeniedQry") // same as in ACL
)

func TestBasicUsage_RowsProcessorFactory(t *testing.T) {
	require := require.New(t)
	department := func(name string) istructs.IStateValue {
		r := &mockRecord{}
		r.
			On("AsString", "name").Return(name).
			On("QName").Return(qNamePosDepartment)
		sv := &mockStateValue{}
		sv.On("AsRecord", "").Return(r)
		return sv
	}
	skb := &mockStateKeyBuilder{}
	skb.On("PutRecordID", mock.Anything, mock.Anything)
	s := &mockState{}
	s.
		On("KeyBuilder", state.Record, appdef.NullQName).Return(skb).
		On("MustExist", mock.Anything).Return(department("Soft drinks")).Once().
		On("MustExist", mock.Anything).Return(department("Alcohol drinks")).Once().
		On("MustExist", mock.Anything).Return(department("Alcohol drinks")).Once().
		On("MustExist", mock.Anything).Return(department("Sweet")).Once()

	appDef := appdef.New()
	departmentObj := appDef.AddObject(qNamePosDepartment)
	departmentObj.AddField("name", appdef.DataKind_string, false)
	resultMeta := appDef.AddObject(appdef.NewQName("pos", "DepartmentResult"))
	resultMeta.
		AddField("id", appdef.DataKind_int64, true).
		AddField("name", appdef.DataKind_string, false)

	params := queryParams{
		elements: []IElement{
			element{
				path: path{rootDocument},
				fields: []IResultField{
					resultField{"id"},
					resultField{"name"},
				},
				refs: []IRefField{
					refField{field: "id_department", ref: "name", key: "id_department/name"},
				},
			},
		},
		count:     1,
		startFrom: 1,
		filters: []IFilter{
			&EqualsFilter{
				field: "id_department/name",
				value: "Alcohol drinks",
			},
		},
		orderBy: []IOrderBy{
			orderBy{
				field: "name",
				desc:  false,
			},
		},
	}
	work := func(id int64, name string, idDepartment int64) pipeline.IWorkpiece {
		return rowsWorkpiece{
			object: &coreutils.TestObject{
				Name: appdef.NewQName("pos", "Article"),
				Data: map[string]interface{}{"id": id, "name": name, "id_department": istructs.RecordID(idDepartment)},
			},
			outputRow: &outputRow{
				keyToIdx: map[string]int{rootDocument: 0},
				values:   make([]interface{}, 1),
			},
			enrichedRootFieldsKinds: make(map[string]appdef.DataKind),
		}
	}

	result := ""
	rs := testResultSenderClosable{
		startArraySection: func(sectionType string, path []string) {},
		sendElement: func(name string, element interface{}) (err error) {
			bb, err := json.Marshal(element)
			result = string(bb)
			return err
		},
		close: func(err error) {},
	}
	processor := ProvideRowsProcessorFactory()(context.Background(), appDef, s, params, resultMeta, rs, &testMetrics{})

	require.NoError(processor.SendAsync(work(1, "Cola", 10)))
	require.NoError(processor.SendAsync(work(3, "White wine", 20)))
	require.NoError(processor.SendAsync(work(2, "Amaretto", 20)))
	require.NoError(processor.SendAsync(work(4, "Cake", 40)))
	processor.Close()

	require.Equal(`[[[3,"White wine","Alcohol drinks"]]]`, result)
}

func getTestCfg(require *require.Assertions, prepareAppDef func(appdef.IAppDefBuilder), cfgFunc ...func(cfg *istructsmem.AppConfigType)) (appDef appdef.IAppDef, asp istructs.IAppStructsProvider, appTokens istructs.IAppTokens) {
	cfgs := make(istructsmem.AppConfigsType)
	asf := mem.Provide()
	storageProvider := istorageimpl.Provide(asf)
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeFunc)

	qNameFindArticlesByModificationTimeStampRangeParams := appdef.NewQName("bo", "FindArticlesByModificationTimeStampRangeParamsDef")
	qNameDepartment := appdef.NewQName("bo", "Department")
	qNameArticle := appdef.NewQName("bo", "Article")

	adb := appdef.New()
	adb.AddObject(qNameFindArticlesByModificationTimeStampRangeParams).
		AddField("from", appdef.DataKind_int64, false).
		AddField("till", appdef.DataKind_int64, false)
	adb.AddCDoc(qNameDepartment).
		AddField("name", appdef.DataKind_string, true)
	adb.AddObject(qNameArticle).
		AddField("sys.ID", appdef.DataKind_RecordID, true).
		AddField("name", appdef.DataKind_string, true).
		AddField("id_department", appdef.DataKind_int64, true)
	adb.AddSingleton(authnz.QNameCDocWorkspaceDescriptor) // need to avoid error cdoc.sys.wsdesc missing
	adb.AddQuery(qNameFunction).SetParam(qNameFindArticlesByModificationTimeStampRangeParams).SetResult(appdef.NewQName("bo", "Article"))
	adb.AddCommand(istructs.QNameCommandCUD)
	adb.AddQuery(qNameQryDenied)

	if prepareAppDef != nil {
		prepareAppDef(adb)
	}

	cfg := cfgs.AddConfig(appName, adb)

	asp = istructsmem.Provide(cfgs, iratesce.TestBucketsFactory, payloads.TestAppTokensFactory(tokens), storageProvider)

	article := func(id, idDepartment istructs.RecordID, name string) istructs.IObject {
		return &coreutils.TestObject{
			Name: appdef.NewQName("bo", "Article"),
			Data: map[string]interface{}{"sys.ID": id, "name": name, "id_department": idDepartment},
		}
	}
	cfg.Resources.Add(istructsmem.NewQueryFunction(
		qNameFunction,
		func(_ context.Context, args istructs.ExecQueryArgs, callback istructs.ExecQueryCallback) (err error) {
			require.Equal(int64(1257894000), args.ArgumentObject.AsInt64("from"))
			require.Equal(int64(2257894000), args.ArgumentObject.AsInt64("till"))
			objects := []istructs.IObject{
				article(1, istructs.MaxRawRecordID+10, "Cola"),
				article(3, istructs.MaxRawRecordID+20, "White wine"),
				article(2, istructs.MaxRawRecordID+20, "Amaretto"),
				article(4, istructs.MaxRawRecordID+40, "Cake"),
			}
			for _, object := range objects {
				err = callback(object)
				if err != nil {
					return err
				}
			}
			return err
		},
	))
	cfg.Resources.Add(istructsmem.NewCommandFunction(istructs.QNameCommandCUD, istructsmem.NullCommandExec))
	cfg.Resources.Add(istructsmem.NewQueryFunction(qNameQryDenied, istructsmem.NullQueryExec))

	for _, f := range cfgFunc {
		f(cfg)
	}

	as, err := asp.AppStructs(appName)
	require.NoError(err)

	appDef = as.AppDef()

	plogOffset := istructs.FirstOffset
	wlogOffset := istructs.FirstOffset
	grebp := istructs.GenericRawEventBuilderParams{
		HandlingPartition: partID,
		Workspace:         wsID,
		QName:             istructs.QNameCommandCUD,
		RegisteredAt:      istructs.UnixMilli(time.Now().UnixMilli()),
		PLogOffset:        plogOffset,
		WLogOffset:        wlogOffset,
	}
	reb := as.Events().GetSyncRawEventBuilder(
		istructs.SyncRawEventBuilderParams{
			GenericRawEventBuilderParams: grebp,
			SyncedAt:                     istructs.UnixMilli(time.Now().UnixMilli()),
		},
	)

	namedDoc := func(qName appdef.QName, id istructs.RecordID, name string) {
		doc := reb.CUDBuilder().Create(qName)
		doc.PutRecordID(appdef.SystemField_ID, id)
		doc.PutString("name", name)
	}
	namedDoc(qNameDepartment, istructs.MaxRawRecordID+10, "Soft drinks")
	namedDoc(qNameDepartment, istructs.MaxRawRecordID+20, "Alcohol drinks")
	namedDoc(qNameDepartment, istructs.MaxRawRecordID+40, "Sweet")

	rawEvent, err := reb.BuildRawEvent()
	require.NoError(err)
	pLogEvent, err := as.Events().PutPlog(rawEvent, nil, istructsmem.NewIDGenerator())
	require.NoError(err)
	require.NoError(as.Records().Apply(pLogEvent))
	err = as.Events().PutWlog(pLogEvent)
	require.NoError(err)
	appTokens = payloads.TestAppTokensFactory(tokens).New(appName)
	return appDef, asp, appTokens
}

func TestBasicUsage_ServiceFactory(t *testing.T) {
	require := require.New(t)
	done := make(chan interface{})
	result := ""
	body := []byte(`{
						"args":{"from":1257894000,"till":2257894000},
						"elements":[
							{"path":"","fields":["sys.ID","name"],"refs":[["id_department","name"]]}
						],
						"filters":[
							{"expr":"and","args":[{"expr":"eq","args":{"field":"id_department/name","value":"Alcohol drinks"}}]},
							{"expr":"or","args":[{"expr":"eq","args":{"field":"id_department/name","value":"Alcohol drinks"}}]}
						],
						"orderBy":[{"field":"name"}],
						"count":1,
						"startFrom":1
					}`)
	serviceChannel := make(iprocbus.ServiceChannel)
	rs := testResultSenderClosable{
		startArraySection: func(sectionType string, path []string) {},
		sendElement: func(name string, element interface{}) (err error) {
			bb, err := json.Marshal(element)
			require.NoError(err)
			result = string(bb)
			return nil
		},
		close: func(err error) {
			require.NoError(err)
			close(done)
		},
	}

	metrics := imetrics.Provide()
	metricNames := make([]string, 0)

	appDef, appStructsProvider, appTokens := getTestCfg(require, nil)

	appParts, cleanAppParts, err := appparts.New(appStructsProvider)
	require.NoError(err)
	defer cleanAppParts()
	appParts.DeployApp(appName, appDef, appPartsCount, appEngines)
	appParts.DeployAppPartitions(appName, []istructs.PartitionID{partID})

	authn := iauthnzimpl.NewDefaultAuthenticator(iauthnzimpl.TestSubjectRolesGetter)
	authz := iauthnzimpl.NewDefaultAuthorizer()
	queryProcessor := ProvideServiceFactory()(
		serviceChannel,
		func(ctx context.Context, sender ibus.ISender) IResultSenderClosable { return rs },
		appParts,
		3, // max concurrent queries
		metrics, "vvm", authn, authz)
	processorCtx, processorCtxCancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		queryProcessor.Run(processorCtx)
		wg.Done()
	}()
	query := appDef.Query(qNameFunction) // nnv: Suspicious code!! Should be borrowed AppPartition.AppDef() instead of appDef?
	systemToken := getSystemToken(appTokens)
	serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", systemToken)
	<-done
	processorCtxCancel()
	wg.Wait()

	_ = metrics.List(func(metric imetrics.IMetric, metricValue float64) (err error) {
		metricNames = append(metricNames, metric.Name())
		return err
	})

	require.Equal(`[[[3,"White wine","Alcohol drinks"]]]`, result)
	require.Contains(metricNames, queriesTotal)
	require.Contains(metricNames, queriesSeconds)
	require.Contains(metricNames, buildSeconds)
	require.Contains(metricNames, execSeconds)
	require.Contains(metricNames, execFieldsSeconds)
	require.Contains(metricNames, execEnrichSeconds)
	require.Contains(metricNames, execFilterSeconds)
	require.Contains(metricNames, execOrderSeconds)
	require.Contains(metricNames, execCountSeconds)
	require.Contains(metricNames, execSendSeconds)
}

func TestRawMode(t *testing.T) {
	require := require.New(t)

	appDef := appdef.New()
	resultMeta := appDef.AddObject(istructs.QNameRaw)

	result := ""
	rs := testResultSenderClosable{
		startArraySection: func(sectionType string, path []string) {},
		sendElement: func(name string, element interface{}) (err error) {
			bb, err := json.Marshal(element)
			result = string(bb)
			return err
		},
		close: func(err error) {},
	}
	processor := ProvideRowsProcessorFactory()(context.Background(), appDef, &mockState{}, queryParams{}, resultMeta, rs, &testMetrics{})

	require.NoError(processor.SendAsync(rowsWorkpiece{
		object: &coreutils.TestObject{
			Data: map[string]interface{}{processors.Field_RawObject_Body: `[accepted]`},
		},
		outputRow: &outputRow{
			keyToIdx: map[string]int{rootDocument: 0},
			values:   make([]interface{}, 1),
		},
	}))
	processor.Close()

	require.Equal(`[[["[accepted]"]]]`, result)
}

func Test_epsilon(t *testing.T) {
	options := func(epsilon interface{}) map[string]interface{} {
		options := make(map[string]interface{})
		if epsilon != nil {
			options["epsilon"] = epsilon
		}
		return options
	}
	args := func(options map[string]interface{}) interface{} {
		args := make(map[string]interface{})
		if options != nil {
			args["options"] = options
		}
		return args
	}
	t.Run("Should return epsilon", func(t *testing.T) {
		epsilon, err := epsilon(args(options(math.E)))

		require.Equal(t, math.E, epsilon)
		require.NoError(t, err)
	})
	t.Run("Should return error when options is nil", func(t *testing.T) {
		//TODO (FILTER0001)
		t.Skip("//TODO (FILTER0001)")
		epsilon, err := epsilon(args(nil))

		require.Equal(t, 0.0, epsilon)
		require.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("Should return error when epsilon is nil", func(t *testing.T) {
		//TODO (FILTER0001)
		t.Skip("//TODO (FILTER0001)")
		epsilon, err := epsilon(args(options(nil)))

		require.Equal(t, 0.0, epsilon)
		require.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("Should return error when epsilon has wrong type", func(t *testing.T) {
		epsilon, err := epsilon(args(options("0.00000001")))

		require.Equal(t, 0.0, epsilon)
		require.ErrorIs(t, err, coreutils.ErrFieldTypeMismatch)
	})
}

func Test_nearlyEqual(t *testing.T) {
	t.Skip("temp skip")
	tests := []struct {
		name    string
		first   float64
		second  float64
		epsilon float64
		want    bool
	}{
		{
			name:    "Regular large numbers 1",
			first:   1000000.0,
			second:  1000001.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Regular large numbers 2",
			first:   1000001.0,
			second:  1000000.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Regular large numbers 3",
			first:   10000.0,
			second:  10001.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Regular large numbers 4",
			first:   10001.0,
			second:  10000.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Negative large numbers 1",
			first:   -1000000.0,
			second:  -1000001.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Negative large numbers 2",
			first:   -1000001.0,
			second:  -1000000.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Negative large numbers 3",
			first:   -10000.0,
			second:  -10001.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Negative large numbers 4",
			first:   -10001.0,
			second:  -10000.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers around one 1",
			first:   1.0000001,
			second:  1.0000002,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers around one 2",
			first:   1.0000002,
			second:  1.0000001,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers around one 3",
			first:   1.0002,
			second:  1.0001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers around one 4",
			first:   1.0001,
			second:  1.0002,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers around minus one 1",
			first:   -1.0000001,
			second:  -1.0000002,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers around minus one 2",
			first:   -1.0000002,
			second:  -1.0000001,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers around minus one 3",
			first:   -1.0002,
			second:  -1.0001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers around minus one 4",
			first:   -1.0001,
			second:  -1.0002,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers between one and zero 1",
			first:   0.000000001000001,
			second:  0.000000001000002,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers between one and zero 2",
			first:   0.000000001000002,
			second:  0.000000001000001,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers between one and zero 3",
			first:   0.000000000001002,
			second:  0.000000000001001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers between one and zero 4",
			first:   0.000000000001001,
			second:  0.000000000001002,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers between minus one and zero 1",
			first:   -0.000000001000001,
			second:  -0.000000001000002,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers between minus one and zero 2",
			first:   -0.000000001000002,
			second:  -0.000000001000001,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Numbers between minus one and zero 3",
			first:   -0.000000000001002,
			second:  -0.000000000001001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Numbers between minus one and zero 4",
			first:   -0.000000000001001,
			second:  -0.000000000001002,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Small differences away from zero 1",
			first:   0.3,
			second:  0.30000003,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Small differences away from zero 2",
			first:   -0.3,
			second:  -0.30000003,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 1",
			first:   0.0,
			second:  0.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 2",
			first:   0.00000001,
			second:  0.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 3",
			first:   0.0,
			second:  0.00000001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 4",
			first:   -0.00000001,
			second:  0.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 5",
			first:   0.0,
			second:  -0.00000001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 6",
			first:   0.0,
			second:  1e-40,
			epsilon: 0.01,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 7",
			first:   1e-40,
			second:  0.0,
			epsilon: 0.01,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 8",
			first:   0.0,
			second:  1e-40,
			epsilon: 0.000001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 9",
			first:   1e-40,
			second:  0.0,
			epsilon: 0.000001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 10",
			first:   0.0,
			second:  -1e-40,
			epsilon: 0.01,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 11",
			first:   -1e-40,
			second:  0.0,
			epsilon: 0.01,
			want:    true,
		},
		{
			name:    "Comparisons involving zero 12",
			first:   0.0,
			second:  -1e-40,
			epsilon: 0.000001,
			want:    false,
		},
		{
			name:    "Comparisons involving zero 13",
			first:   -1e-40,
			second:  0.0,
			epsilon: 0.000001,
			want:    false,
		},
		{
			name:    "Comparisons involving extreme values 1",
			first:   math.MaxFloat64,
			second:  math.MaxFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons involving extreme values 2",
			first:   math.MaxFloat64,
			second:  -math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving extreme values 3",
			first:   -math.MaxFloat64,
			second:  math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving extreme values 4",
			first:   math.MaxFloat64,
			second:  math.MaxFloat64 / 2,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving extreme values 5",
			first:   math.MaxFloat64,
			second:  -math.MaxFloat64 / 2,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving extreme values 6",
			first:   -math.MaxFloat64,
			second:  math.MaxFloat64 / 2,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving infinities 1",
			first:   math.Inf(+1),
			second:  math.Inf(+1),
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons involving infinities 2",
			first:   math.Inf(-1),
			second:  math.Inf(-1),
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons involving infinities 3",
			first:   math.Inf(-1),
			second:  math.Inf(+1),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving infinities 4",
			first:   math.Inf(+1),
			second:  math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving infinities 5",
			first:   math.Inf(-1),
			second:  -math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 1",
			first:   math.NaN(),
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 2",
			first:   math.NaN(),
			second:  0.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 3",
			first:   0.0,
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 4",
			first:   math.NaN(),
			second:  math.Inf(+1),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 5",
			first:   math.Inf(+1),
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 6",
			first:   math.NaN(),
			second:  math.Inf(-1),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 7",
			first:   math.Inf(-1),
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 8",
			first:   math.NaN(),
			second:  math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 9",
			first:   math.MaxFloat64,
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 10",
			first:   math.NaN(),
			second:  -math.MaxFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 11",
			first:   -math.MaxFloat64,
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 12",
			first:   math.NaN(),
			second:  math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 13",
			first:   math.SmallestNonzeroFloat64,
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 14",
			first:   math.NaN(),
			second:  -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons involving NaN values 15",
			first:   -math.SmallestNonzeroFloat64,
			second:  math.NaN(),
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 1",
			first:   1.000000001,
			second:  -1.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 2",
			first:   -1.0,
			second:  1.000000001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 3",
			first:   -1.000000001,
			second:  1.0,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 4",
			first:   1.0,
			second:  -1.000000001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 5",
			first:   10 * math.SmallestNonzeroFloat64,
			second:  10 * -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers on opposite sides of zero 6",
			first:   10000 * math.SmallestNonzeroFloat64,
			second:  10000 * -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers very close to zero 1",
			first:   math.SmallestNonzeroFloat64,
			second:  math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 2",
			first:   math.SmallestNonzeroFloat64,
			second:  -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 3",
			first:   -math.SmallestNonzeroFloat64,
			second:  math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 4",
			first:   math.SmallestNonzeroFloat64,
			second:  0.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 5",
			first:   0.0,
			second:  math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 6",
			first:   -math.SmallestNonzeroFloat64,
			second:  0.0,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 7",
			first:   0.0,
			second:  -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    true,
		},
		{
			name:    "Comparisons of numbers very close to zero 8",
			first:   0.000000001,
			second:  -math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers very close to zero 9",
			first:   0.000000001,
			second:  math.SmallestNonzeroFloat64,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers very close to zero 10",
			first:   math.SmallestNonzeroFloat64,
			second:  0.000000001,
			epsilon: 0.00001,
			want:    false,
		},
		{
			name:    "Comparisons of numbers very close to zero 11",
			first:   -math.SmallestNonzeroFloat64,
			second:  0.000000001,
			epsilon: 0.00001,
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, nearlyEqual(test.first, test.second, test.epsilon))
		})
	}
}

func TestRateLimiter(t *testing.T) {
	require := require.New(t)
	errs := make(chan error)
	serviceChannel := make(iprocbus.ServiceChannel)
	rs := testResultSenderClosable{
		startArraySection: func(sectionType string, path []string) {},
		sendElement:       func(name string, element interface{}) (err error) { return nil },
		close: func(err error) {
			errs <- err
		},
	}

	qNameMyFuncParams := appdef.NewQName(appdef.SysPackage, "myFuncParams")
	qNameMyFuncResults := appdef.NewQName(appdef.SysPackage, "results")
	qName := appdef.NewQName(appdef.SysPackage, "myFunc")
	appDef, appStructsProvider, appTokens := getTestCfg(require,
		func(appDef appdef.IAppDefBuilder) {
			appDef.AddObject(qNameMyFuncParams)
			appDef.AddObject(qNameMyFuncResults).
				AddField("fld", appdef.DataKind_string, false)
			appDef.AddQuery(qName).SetParam(qNameMyFuncParams).SetResult(qNameMyFuncResults)
		},
		func(cfg *istructsmem.AppConfigType) {
			myFunc := istructsmem.NewQueryFunction(qName, istructsmem.NullQueryExec)
			// declare a test func

			cfg.Resources.Add(myFunc)

			// declare rate limits
			cfg.FunctionRateLimits.AddWorkspaceLimit(qName, istructs.RateLimit{
				Period:                time.Minute,
				MaxAllowedPerDuration: 2,
			})
		})

	appParts, cleanAppParts, err := appparts.New(appStructsProvider)
	require.NoError(err)
	defer cleanAppParts()
	appParts.DeployApp(appName, appDef, appPartsCount, appEngines)
	appParts.DeployAppPartitions(appName, []istructs.PartitionID{partID})

	// create aquery processor
	metrics := imetrics.Provide()
	authn := iauthnzimpl.NewDefaultAuthenticator(iauthnzimpl.TestSubjectRolesGetter)
	authz := iauthnzimpl.NewDefaultAuthorizer()
	queryProcessor := ProvideServiceFactory()(
		serviceChannel,
		func(ctx context.Context, sender ibus.ISender) IResultSenderClosable { return rs },
		appParts,
		3, // max concurrent queries
		metrics, "vvm", authn, authz)
	go queryProcessor.Run(context.Background())

	systemToken := getSystemToken(appTokens)
	body := []byte(`{
		"args":{},
		"elements":[{"path":"","fields":["fld"]}]
	}`)

	// execute query
	// first 2 - ok
	query := appDef.Query(qName)
	serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", systemToken)
	require.NoError(<-errs)
	serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", systemToken)
	require.NoError(<-errs)

	// 3rd exceeds the limit - not often than twice per minute
	serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", systemToken)
	require.Error(<-errs)
}

func TestAuthnz(t *testing.T) {
	require := require.New(t)
	errs := make(chan error)
	body := []byte(`{}`)
	serviceChannel := make(iprocbus.ServiceChannel)
	rs := testResultSenderClosable{
		startArraySection: func(sectionType string, path []string) {},
		sendElement: func(name string, element interface{}) (err error) {
			t.Fail()
			return nil
		},
		close: func(err error) {
			errs <- err
		},
	}

	metrics := imetrics.Provide()

	appDef, appStructsProvider, appTokens := getTestCfg(require, nil)

	appParts, cleanAppParts, err := appparts.New(appStructsProvider)
	require.NoError(err)
	defer cleanAppParts()

	appParts.DeployApp(appName, appDef, appPartsCount, appEngines)
	appParts.DeployAppPartitions(appName, []istructs.PartitionID{partID})

	authn := iauthnzimpl.NewDefaultAuthenticator(iauthnzimpl.TestSubjectRolesGetter)
	authz := iauthnzimpl.NewDefaultAuthorizer()
	queryProcessor := ProvideServiceFactory()(
		serviceChannel,
		func(ctx context.Context, sender ibus.ISender) IResultSenderClosable { return rs },
		appParts,
		3, // max concurrent queries
		metrics, "vvm", authn, authz)
	go queryProcessor.Run(context.Background())
	query := appDef.Query(qNameFunction)

	t.Run("no token for a query that requires authorization -> 403 unauthorized", func(t *testing.T) {
		serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", "")
		var se coreutils.SysError
		require.ErrorAs(<-errs, &se)
		require.Equal(http.StatusForbidden, se.HTTPStatus)
	})

	t.Run("expired token -> 401 unauthorized", func(t *testing.T) {
		systemToken := getSystemToken(appTokens)
		// make the token be expired
		now = now.Add(2 * time.Minute)
		serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsID, nil, body, query, "127.0.0.1", systemToken)
		var se coreutils.SysError
		require.ErrorAs(<-errs, &se)
		require.Equal(http.StatusUnauthorized, se.HTTPStatus)
	})

	t.Run("token provided by querying is denied -> 403 forbidden", func(t *testing.T) {
		wsid := istructs.WSID(1)
		token := getTestToken(appTokens, wsid)
		deniedQuery := appDef.Query(qNameQryDenied) // nnv: Suspicious code!!
		serviceChannel <- NewQueryMessage(context.Background(), appName, partID, wsid, nil, body, deniedQuery, "127.0.0.1", token)
		var se coreutils.SysError
		require.ErrorAs(<-errs, &se)
		require.Equal(http.StatusForbidden, se.HTTPStatus)
	})
}

type testOutputRow struct {
	fields     []string
	fieldToIdx map[string]int
	values     []interface{}
}

func (r *testOutputRow) Set(alias string, value interface{}) {
	if r.values == nil {
		r.values = make([]interface{}, len(r.fields))
		r.fieldToIdx = make(map[string]int)
		for i, field := range r.fields {
			r.fieldToIdx[field] = i
		}
	}
	r.values[r.fieldToIdx[alias]] = value
}

func (r testOutputRow) Value(alias string) interface{} { return r.values[r.fieldToIdx[alias]] }
func (r testOutputRow) Values() []interface{}          { return r.values }

type testFilter struct {
	match bool
	err   error
}

func (f testFilter) IsMatch(FieldsKinds, IOutputRow) (bool, error) {
	return f.match, f.err
}

type testWorkpiece struct {
	object    istructs.IObject
	outputRow IOutputRow
	release   func()
}

func (w testWorkpiece) Object() istructs.IObject { return w.object }
func (w testWorkpiece) OutputRow() IOutputRow    { return w.outputRow }
func (w testWorkpiece) EnrichedRootFieldsKinds() FieldsKinds {
	return FieldsKinds{}
}
func (w testWorkpiece) PutEnrichedRootFieldKind(string, appdef.DataKind) {
	panic("implement me")
}
func (w testWorkpiece) Release() {
	if w.release != nil {
		w.release()
	}
}

type testResultSenderClosable struct {
	startArraySection func(sectionType string, path []string)
	objectSection     func(sectionType string, path []string, element interface{}) (err error)
	sendElement       func(name string, element interface{}) (err error)
	close             func(err error)
}

func (s testResultSenderClosable) StartArraySection(sectionType string, path []string) {
	s.startArraySection(sectionType, path)
}
func (s testResultSenderClosable) StartMapSection(string, []string) { panic("implement me") }
func (s testResultSenderClosable) ObjectSection(sectionType string, path []string, element interface{}) (err error) {
	return s.objectSection(sectionType, path, element)
}
func (s testResultSenderClosable) SendElement(name string, element interface{}) (err error) {
	return s.sendElement(name, element)
}
func (s testResultSenderClosable) Close(err error) { s.close(err) }

type testMetrics struct{}

func (m *testMetrics) Increase(string, float64) {}

func getTestToken(appTokens istructs.IAppTokens, wsid istructs.WSID) string {
	pp := payloads.PrincipalPayload{
		Login:       "syslogin",
		SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: wsid,
	}
	token, err := appTokens.IssueToken(time.Minute, &pp)
	if err != nil {
		panic(err)
	}
	return token
}

func getSystemToken(appTokens istructs.IAppTokens) string {
	pp := payloads.PrincipalPayload{
		Login:       "syslogin",
		SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: istructs.NullWSID,
	}
	token, err := appTokens.IssueToken(time.Minute, &pp)
	if err != nil {
		panic(err)
	}
	return token
}

type mockState struct {
	istructs.IState
	mock.Mock
}

func (s *mockState) KeyBuilder(storage, entity appdef.QName) (builder istructs.IStateKeyBuilder, err error) {
	return s.Called(storage, entity).Get(0).(istructs.IStateKeyBuilder), err
}

func (s *mockState) MustExist(key istructs.IStateKeyBuilder) (value istructs.IStateValue, err error) {
	return s.Called(key).Get(0).(istructs.IStateValue), err
}

type mockStateKeyBuilder struct {
	istructs.IStateKeyBuilder
	mock.Mock
}

func (b *mockStateKeyBuilder) PutRecordID(name string, value istructs.RecordID) {
	b.Called(name, value)
}

type mockStateValue struct {
	istructs.IStateValue
	mock.Mock
}

func (o *mockStateValue) AsRecord(name string) istructs.IRecord {
	return o.Called(name).Get(0).(istructs.IRecord)
}

type mockRecord struct {
	istructs.IRecord
	mock.Mock
}

func (r *mockRecord) AsString(name string) string { return r.Called(name).String(0) }
func (r *mockRecord) QName() appdef.QName         { return r.Called().Get(0).(appdef.QName) }
