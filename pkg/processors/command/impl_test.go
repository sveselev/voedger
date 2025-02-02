/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package commandprocessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/iauthnzimpl"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/in10nmem"
	"github.com/voedger/voedger/pkg/iratesce"
	"github.com/voedger/voedger/pkg/isecretsimpl"
	"github.com/voedger/voedger/pkg/istorage/mem"
	istorageimpl "github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/itokensjwt"
	imetrics "github.com/voedger/voedger/pkg/metrics"
	"github.com/voedger/voedger/pkg/pipeline"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/projectors"
	coreutils "github.com/voedger/voedger/pkg/utils"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
	"github.com/voedger/voedger/staging/src/github.com/untillpro/ibusmem"
)

var (
	testCRecord = appdef.NewQName("test", "TestCRecord")
	testCDoc    = appdef.NewQName("test", "TestCDoc")
	testWDoc    = appdef.NewQName("test", "TestWDoc")
)

func TestBasicUsage(t *testing.T) {
	require := require.New(t)
	check := make(chan interface{}, 1)
	cudsCheck := make(chan interface{})

	testCmdQName := appdef.NewQName(appdef.SysPackage, "Test")
	// схема параметров тестовой команды
	testCmdQNameParams := appdef.NewQName(appdef.SysPackage, "TestParams")
	// схема unlogged-параметров тестовой команды
	testCmdQNameParamsUnlogged := appdef.NewQName(appdef.SysPackage, "TestParamsUnlogged")
	prepareAppDef := func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		pars := appDef.AddObject(testCmdQNameParams)
		pars.AddField("Text", appdef.DataKind_string, true)

		unloggedPars := appDef.AddObject(testCmdQNameParamsUnlogged)
		unloggedPars.AddField("Password", appdef.DataKind_string, true)

		appDef.AddCDoc(testCDoc).AddContainer("TestCRecord", testCRecord, 0, 1)
		appDef.AddCRecord(testCRecord)
		appDef.AddCommand(testCmdQName).SetUnloggedParam(testCmdQNameParamsUnlogged).SetParam(testCmdQNameParams)
	}

	app := setUp(t, prepareAppDef)
	defer tearDown(app)

	channelID, err := app.n10nBroker.NewChannel("test", 24*time.Hour)
	require.NoError(err)
	projectionKey := in10n.ProjectionKey{
		App:        istructs.AppQName_untill_airs_bp,
		Projection: projectors.PLogUpdatesQName,
		WS:         1,
	}
	go app.n10nBroker.WatchChannel(app.ctx, channelID, func(projection in10n.ProjectionKey, _ istructs.Offset) {
		require.Equal(projectionKey, projection)
		check <- 1
	})
	app.n10nBroker.Subscribe(channelID, projectionKey)
	defer app.n10nBroker.Unsubscribe(channelID, projectionKey)

	// сама тестовая команда
	testExec := func(args istructs.ExecCommandArgs) (err error) {
		cuds := args.Workpiece.(*cmdWorkpiece).parsedCUDs
		if len(cuds) > 0 {
			require.True(len(cuds) == 1)
			require.Equal(float64(1), cuds[0].fields[appdef.SystemField_ID])
			require.Equal(testCDoc.String(), cuds[0].fields[appdef.SystemField_QName])
			close(cudsCheck)
		}
		require.Equal(istructs.WSID(1), args.PrepareArgs.Workspace)
		require.NotNil(args.State)

		// просто проверим, что мы получили то, что передал клиент
		text := args.ArgumentObject.AsString("Text")
		if text == "fire error" {
			return errors.New(text)
		} else {
			require.Equal("hello", text)
		}
		require.Equal("pass", args.ArgumentUnloggedObject.AsString("Password"))

		check <- 1 // сигнал: проверки случились
		return
	}
	testCmd := istructsmem.NewCommandFunction(testCmdQName, testExec)
	app.cfg.Resources.Add(testCmd)

	t.Run("basic usage", func(t *testing.T) {
		// command processor работает через ibus.SendResponse -> нам нужен sender -> тестируем через ibus.SendRequest2()
		request := ibus.Request{
			Body:     []byte(`{"args":{"Text":"hello"},"unloggedArgs":{"Password":"pass"},"cuds":[{"fields":{"sys.ID":1,"sys.QName":"test.TestCDoc"}}]}`),
			AppQName: istructs.AppQName_untill_airs_bp.String(),
			WSID:     1,
			Resource: "c.sys.Test",
			// need to authorize, otherwise execute will be forbidden
			Header: app.sysAuthHeader,
		}
		resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, request, coreutils.GetTestBustTimeout())
		require.Nil(err, err)
		require.Nil(secErr, secErr)
		require.Nil(sections)
		log.Println(string(resp.Data))
		require.Equal(http.StatusOK, resp.StatusCode)
		require.Equal(coreutils.ApplicationJSON, resp.ContentType)
		// убедимся, что команда действительно отработала и нотификации отправились
		<-check
		<-check

		// убедимся, что CUD'ы проверились
		<-cudsCheck
	})

	t.Run("500 internal server error command exec error", func(t *testing.T) {
		request := ibus.Request{
			Body:     []byte(`{"args":{"Text":"fire error"},"unloggedArgs":{"Password":"pass"}}`),
			AppQName: istructs.AppQName_untill_airs_bp.String(),
			WSID:     1,
			Resource: "c.sys.Test",
			Header:   app.sysAuthHeader,
		}
		resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, request, coreutils.GetTestBustTimeout())
		require.Nil(err, err)
		require.Nil(secErr, secErr)
		require.Nil(sections)
		require.Equal(http.StatusInternalServerError, resp.StatusCode)
		require.Equal(coreutils.ApplicationJSON, resp.ContentType)
		require.Equal(`{"sys.Error":{"HTTPStatus":500,"Message":"fire error"}}`, string(resp.Data))
		require.Contains(string(resp.Data), "fire error")
		log.Println(string(resp.Data))
	})
}

func sendCUD(t *testing.T, wsid istructs.WSID, app testApp, expectedCode ...int) map[string]interface{} {
	require := require.New(t)
	req := ibus.Request{
		WSID:     int64(wsid),
		AppQName: istructs.AppQName_untill_airs_bp.String(),
		Resource: "c.sys.CUD",
		Body: []byte(`{"cuds":[
			{"fields":{"sys.ID":1,"sys.QName":"test.TestCDoc"}},
			{"fields":{"sys.ID":2,"sys.QName":"test.TestWDoc"}},
			{"fields":{"sys.ID":3,"sys.QName":"test.TestCRecord","sys.ParentID":1,"sys.Container":"TestCRecord"}}
		]}`),
		Header: app.sysAuthHeader,
	}
	resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
	require.Nil(err, err)
	require.Nil(secErr, secErr)
	require.Nil(sections)
	if len(expectedCode) == 0 {
		require.Equal(http.StatusOK, resp.StatusCode)
	} else {
		require.Equal(expectedCode[0], resp.StatusCode)
	}
	respData := map[string]interface{}{}
	require.Nil(json.Unmarshal(resp.Data, &respData))
	return respData
}

func TestRecoveryOnProjectorError(t *testing.T) {
	require := require.New(t)

	cudQName := appdef.NewQName(appdef.SysPackage, "CUD")
	testErr := errors.New("test error")
	counter := 0
	app := setUp(t, func(appDef appdef.IAppDefBuilder, cfg *istructsmem.AppConfigType) {
		appDef.AddCRecord(testCRecord)
		appDef.AddCDoc(testCDoc).AddContainer("TestCRecord", testCRecord, 0, 1)
		appDef.AddWDoc(testWDoc)
		appDef.AddCommand(cudQName)

		failingProjQName := appdef.NewQName(appdef.SysPackage, "Failer")
		cfg.AddSyncProjectors(func(partition istructs.PartitionID) istructs.Projector {
			return istructs.Projector{
				Name: failingProjQName,
				Func: func(event istructs.IPLogEvent, state istructs.IState, intents istructs.IIntents) (err error) {
					counter++
					if counter == 2 {
						return testErr
					}
					return nil
				},
			}
		})
		appDef.AddProjector(failingProjQName).AddEvent(cudQName, appdef.ProjectorEventKind_Execute)
	})
	defer tearDown(app)

	cmdCUD := istructsmem.NewCommandFunction(cudQName, istructsmem.NullCommandExec)
	app.cfg.Resources.Add(cmdCUD)

	// ok to c.sys.CUD
	respData := sendCUD(t, 1, app)
	require.Equal(1, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+1, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

	// 2nd c.sys.CUD -> sync projector failure, expect 500 internal server error
	respData = sendCUD(t, 1, app, http.StatusInternalServerError)
	require.Equal(testErr.Error(), respData["sys.Error"].(map[string]interface{})["Message"].(string))

	// PLog and record is applied but WLog is not written here because sync projector is failed
	// partition is scheduled to be recovered

	// 3rd c.sys.CUD - > recovery procedure must re-apply 2nd event (PLog, records and WLog), then 3rd event is processed ok (sync projectors are ok)
	respData = sendCUD(t, 1, app)
	require.Equal(3, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+4, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID)+2, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+5, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

}

func TestRecovery(t *testing.T) {
	require := require.New(t)

	cudQName := appdef.NewQName(appdef.SysPackage, "CUD")
	app := setUp(t, func(appDef appdef.IAppDefBuilder, cfg *istructsmem.AppConfigType) {
		appDef.AddCRecord(testCRecord)
		appDef.AddCDoc(testCDoc).AddContainer("TestCRecord", testCRecord, 0, 1)
		appDef.AddWDoc(testWDoc)
		appDef.AddCommand(cudQName)
	})
	defer tearDown(app)

	cmdCUD := istructsmem.NewCommandFunction(cudQName, istructsmem.NullCommandExec)
	app.cfg.Resources.Add(cmdCUD)

	respData := sendCUD(t, 1, app)
	require.Equal(1, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+1, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

	restartCmdProc(&app)
	respData = sendCUD(t, 1, app)
	require.Equal(2, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+2, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID)+1, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+3, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

	restartCmdProc(&app)
	respData = sendCUD(t, 2, app)
	require.Equal(1, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID), istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+1, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

	restartCmdProc(&app)
	respData = sendCUD(t, 1, app)
	require.Equal(3, int(respData["CurrentWLogOffset"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+4, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["1"].(float64)))
	require.Equal(istructs.NewRecordID(istructs.FirstBaseRecordID)+2, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["2"].(float64)))
	require.Equal(istructs.NewCDocCRecordID(istructs.FirstBaseRecordID)+5, istructs.RecordID(respData["NewIDs"].(map[string]interface{})["3"].(float64)))

	app.cancel()
	<-app.done
}

func restartCmdProc(app *testApp) {
	app.cancel()
	<-app.done
	app.ctx, app.cancel = context.WithCancel(context.Background())
	app.done = make(chan struct{})
	go func() {
		app.cmdProcService.Run(app.ctx)
		close(app.done)
	}()
}

func TestCUDUpdate(t *testing.T) {
	require := require.New(t)

	testQName := appdef.NewQName("test", "test")

	cudQName := appdef.NewQName(appdef.SysPackage, "CUD")
	app := setUp(t, func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		_ = appDef.AddCDoc(testQName).AddField("IntFld", appdef.DataKind_int32, false)
		_ = appDef.AddCommand(cudQName)
	})
	defer tearDown(app)

	cmdCUD := istructsmem.NewCommandFunction(cudQName, istructsmem.NullCommandExec)
	app.cfg.Resources.Add(cmdCUD)

	// insert
	req := ibus.Request{
		WSID:     1,
		AppQName: istructs.AppQName_untill_airs_bp.String(),
		Resource: "c.sys.CUD",
		Body:     []byte(`{"cuds":[{"fields":{"sys.ID":1,"sys.QName":"test.test"}}]}`),
		Header:   app.sysAuthHeader,
	}
	resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
	require.Nil(err, err)
	require.Nil(secErr, secErr)
	require.Nil(sections)
	require.Equal(http.StatusOK, resp.StatusCode)
	require.Equal(coreutils.ApplicationJSON, resp.ContentType)
	m := map[string]interface{}{}
	require.NoError(json.Unmarshal(resp.Data, &m))

	t.Run("update", func(t *testing.T) {
		id := int64(m["NewIDs"].(map[string]interface{})["1"].(float64))
		req.Body = []byte(fmt.Sprintf(`{"cuds":[{"sys.ID":%d,"fields":{"sys.QName":"test.test", "IntFld": 42}}]}`, id))
		resp, sections, secErr, err = app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
		require.Nil(err, err)
		require.Nil(secErr, secErr)
		require.Nil(sections)
		require.Equal(http.StatusOK, resp.StatusCode)
		require.Equal(coreutils.ApplicationJSON, resp.ContentType)
	})

	t.Run("404 not found on update not existing", func(t *testing.T) {
		req.Body = []byte(fmt.Sprintf(`{"cuds":[{"sys.ID":%d,"fields":{"sys.QName":"test.test", "IntFld": 42}}]}`, istructs.NonExistingRecordID))
		resp, sections, secErr, err = app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
		require.Nil(err, err)
		require.Nil(secErr, secErr)
		require.Nil(sections)
		require.Equal(http.StatusNotFound, resp.StatusCode)
		require.Equal(coreutils.ApplicationJSON, resp.ContentType)
	})
}

func Test400BadRequestOnCUDErrors(t *testing.T) {
	require := require.New(t)

	testQName := appdef.NewQName("test", "test")

	cudQName := appdef.NewQName(appdef.SysPackage, "CUD")
	app := setUp(t, func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		_ = appDef.AddCDoc(testQName)
		_ = appDef.AddCommand(cudQName)
	})
	defer tearDown(app)

	cmdCUD := istructsmem.NewCommandFunction(cudQName, istructsmem.NullCommandExec)
	app.cfg.Resources.Add(cmdCUD)

	cases := []struct {
		desc                string
		bodyAdd             string
		expectedMessageLike string
	}{
		{"not an object", `"cuds":42`, `'cuds' must be an array of objects`},
		{`element is not an object`, `"cuds":[42]`, `cuds[0]: not an object`},
		{`missing fields`, `"cuds":[{}]`, `cuds[0]: "fields" missing`},
		{`fields is not an object`, `"cuds":[{"fields":42}]`, `cuds[0]: field 'fields' must be an object`},
		{`fields: sys.ID missing`, `"cuds":[{"fields":{"sys.QName":"test.Test"}}]`, `cuds[0]: "sys.ID" missing`},
		{`fields: sys.ID is not a number (create)`, `"cuds":[{"sys.ID":"wrong","fields":{"sys.QName":"test.Test"}}]`, `cuds[0]: field 'sys.ID' must be an int64`},
		{`fields: sys.ID is not a number (update)`, `"cuds":[{"fields":{"sys.ID":"wrong","sys.QName":"test.Test"}}]`, `cuds[0]: field 'sys.ID' must be an int64`},
		{`fields: wrong qName`, `"cuds":[{"fields":{"sys.ID":1,"sys.QName":"wrong"}},{"fields":{"sys.ID":1,"sys.QName":"test.Test"}}]`, `invalid string representation of qualified name: wrong`},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			req := ibus.Request{
				WSID:     1,
				AppQName: istructs.AppQName_untill_airs_bp.String(),
				Resource: "c.sys.CUD",
				Body:     []byte("{" + c.bodyAdd + "}"),
				Header:   app.sysAuthHeader,
			}
			resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
			require.Nil(err, err)
			require.Nil(secErr, secErr)
			require.Nil(sections)
			require.Equal(http.StatusBadRequest, resp.StatusCode, c.desc)
			require.Equal(coreutils.ApplicationJSON, resp.ContentType, c.desc)
			require.Contains(string(resp.Data), jsonEscape(c.expectedMessageLike), c.desc)
			require.Contains(string(resp.Data), `"HTTPStatus":400`, c.desc)
		})
	}
}

func Test400BadRequests(t *testing.T) {
	require := require.New(t)

	testCmdQNameParams := appdef.NewQName(appdef.SysPackage, "TestParams")
	testCmdQNameParamsUnlogged := appdef.NewQName(appdef.SysPackage, "TestParamsUnlogged")

	testCmdQName := appdef.NewQName(appdef.SysPackage, "Test")
	app := setUp(t, func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		appDef.AddObject(testCmdQNameParams).
			AddField("Text", appdef.DataKind_string, true)

		appDef.AddObject(testCmdQNameParamsUnlogged).
			AddField("Password", appdef.DataKind_string, true)

		appDef.AddCommand(testCmdQName).SetUnloggedParam(testCmdQNameParamsUnlogged).SetParam(testCmdQNameParams)
	})
	defer tearDown(app)

	testCmd := istructsmem.NewCommandFunction(testCmdQName, func(args istructs.ExecCommandArgs) (err error) {
		_ = args.ArgumentObject.AsString("Text")
		_ = args.ArgumentUnloggedObject.AsString("Password")
		return nil
	})
	app.cfg.Resources.Add(testCmd)

	baseReq := ibus.Request{
		WSID:     1,
		AppQName: istructs.AppQName_untill_airs_bp.String(),
		Resource: "c.sys.Test",
		Body:     []byte(`{"args":{"Text":"hello"},"unloggedArgs":{"Password":"123"}}`),
		Header:   app.sysAuthHeader,
	}

	cases := []struct {
		desc string
		ibus.Request
		expectedMessageLike string
	}{
		{"unknown app", ibus.Request{AppQName: "untill/unknown"}, "application untill/unknown not found"}, // TODO: simplify
		{"bad request body", ibus.Request{Body: []byte("{wrong")}, "failed to unmarshal request body: invalid character 'w' looking for beginning of object key string"},
		{"unknown func", ibus.Request{Resource: "c.sys.Unknown"}, "unknown function"},
		{"args: field of wrong type provided", ibus.Request{Body: []byte(`{"args":{"Text":42}}`)}, "wrong field type"},
		{"args: not an object", ibus.Request{Body: []byte(`{"args":42}`)}, `"args" field must be an object`},
		{"args: missing at all with a required field", ibus.Request{Body: []byte(`{}`)}, ""},
		{"unloggedArgs: not an object", ibus.Request{Body: []byte(`{"unloggedArgs":42,"args":{"Text":"txt"}}`)}, `"unloggedArgs" field must be an object`},
		{"unloggedArgs: field of wrong type provided", ibus.Request{Body: []byte(`{"unloggedArgs":{"Password":42},"args":{"Text":"txt"}}`)}, "wrong field type"},
		{"unloggedArgs: missing required field of unlogged args, no unlogged args at all", ibus.Request{Body: []byte(`{"args":{"Text":"txt"}}`)}, ""},
		{"cuds: not an object", ibus.Request{Body: []byte(`{"args":{"Text":"hello"},"unloggedArgs":{"Password":"123"},"cuds":42}`)}, `field 'cuds' must be an array of objects`},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			req := baseReq
			req.Body = make([]byte, len(baseReq.Body))
			copy(req.Body, baseReq.Body)
			if len(c.AppQName) > 0 {
				req.AppQName = c.AppQName
			}
			if len(c.Body) > 0 {
				req.Body = make([]byte, len(c.Body))
				copy(req.Body, c.Body)
			}
			if len(c.Resource) > 0 {
				req.Resource = c.Resource
			}
			resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, req, coreutils.GetTestBustTimeout())
			require.Nil(err, err)
			require.Nil(secErr, secErr)
			require.Nil(sections)
			require.Equal(http.StatusBadRequest, resp.StatusCode)
			require.Equal(coreutils.ApplicationJSON, resp.ContentType)
			require.Contains(string(resp.Data), jsonEscape(c.expectedMessageLike))
			require.Contains(string(resp.Data), `"HTTPStatus":400`, c.desc)
		})
	}
}

func TestAuthnz(t *testing.T) {
	require := require.New(t)

	qNameTestDeniedCDoc := appdef.NewQName(appdef.SysPackage, "TestDeniedCDoc") // the same in core/iauthnzimpl

	qNameAllowedCmd := appdef.NewQName(appdef.SysPackage, "TestAllowedCmd")
	qNameDeniedCmd := appdef.NewQName(appdef.SysPackage, "TestDeniedCmd") // the same in core/iauthnzimpl
	app := setUp(t, func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		appDef.AddCDoc(qNameTestDeniedCDoc)
		appDef.AddCommand(qNameAllowedCmd)
		appDef.AddCommand(qNameDeniedCmd)
	})
	defer tearDown(app)

	app.cfg.Resources.Add(istructsmem.NewCommandFunction(qNameDeniedCmd, istructsmem.NullCommandExec))
	app.cfg.Resources.Add(istructsmem.NewCommandFunction(qNameAllowedCmd, istructsmem.NullCommandExec))

	pp := payloads.PrincipalPayload{
		Login:       "testlogin",
		SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: 1,
	}
	token, err := app.appTokens.IssueToken(10*time.Second, &pp)
	require.NoError(err)

	type testCase struct {
		desc               string
		req                ibus.Request
		expectedStatusCode int
	}
	cases := []testCase{
		{
			desc: "403 on cmd EXECUTE forbidden", req: ibus.Request{
				Body:     []byte(`{}`),
				AppQName: istructs.AppQName_untill_airs_bp.String(),
				WSID:     1,
				Resource: "c.sys.TestDeniedCmd",
				Header:   getAuthHeader(token),
			},
			expectedStatusCode: http.StatusForbidden,
		},
		{
			desc: "403 on INSERT CUD forbidden", req: ibus.Request{
				Body:     []byte(`{"cuds":[{"fields":{"sys.ID":1,"sys.QName":"sys.TestDeniedCDoc"}}]}`),
				AppQName: istructs.AppQName_untill_airs_bp.String(),
				WSID:     1,
				Resource: "c.sys.TestAllowedCmd",
				Header:   getAuthHeader(token),
			},
			expectedStatusCode: http.StatusForbidden,
		},
		{
			desc: "403 if no token for a func that requires authentication", req: ibus.Request{
				Body:     []byte(`{}`),
				AppQName: istructs.AppQName_untill_airs_bp.String(),
				WSID:     1,
				Resource: "c.sys.TestAllowedCmd",
			},
			expectedStatusCode: http.StatusForbidden,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, c.req, coreutils.GetTestBustTimeout())
			require.Nil(err, err)
			require.Nil(secErr, secErr)
			require.Nil(sections)
			log.Println(string(resp.Data))
			require.Equal(c.expectedStatusCode, resp.StatusCode)
		})
	}
}

func getAuthHeader(token string) map[string][]string {
	return map[string][]string{
		coreutils.Authorization: {
			"Bearer " + token,
		},
	}
}

func TestBasicUsage_FuncWithRawArg(t *testing.T) {
	require := require.New(t)
	testCmdQName := appdef.NewQName(appdef.SysPackage, "Test")
	app := setUp(t, func(appDef appdef.IAppDefBuilder, _ *istructsmem.AppConfigType) {
		appDef.AddCommand(testCmdQName).SetParam(istructs.QNameRaw)
	})
	defer tearDown(app)

	ch := make(chan interface{})
	testExec := func(args istructs.ExecCommandArgs) (err error) {
		require.EqualValues("custom content", args.ArgumentObject.AsString(processors.Field_RawObject_Body))
		close(ch)
		return
	}
	testCmd := istructsmem.NewCommandFunction(testCmdQName, testExec)

	app.cfg.Resources.Add(testCmd)

	request := ibus.Request{
		Body:     []byte(`custom content`),
		AppQName: istructs.AppQName_untill_airs_bp.String(),
		WSID:     1,
		Resource: "c.sys.Test",
		Header:   app.sysAuthHeader,
	}
	resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, request, coreutils.GetTestBustTimeout())
	require.Nil(err, err)
	require.Nil(secErr)
	require.Nil(sections)
	require.Equal(http.StatusOK, resp.StatusCode)
	require.Equal(coreutils.ApplicationJSON, resp.ContentType)
	<-ch
}

func TestRateLimit(t *testing.T) {
	require := require.New(t)

	qName := appdef.NewQName(appdef.SysPackage, "MyCmd")
	parsQName := appdef.NewQName(appdef.SysPackage, "Params")

	app := setUp(t,
		func(appDef appdef.IAppDefBuilder, cfg *istructsmem.AppConfigType) {
			appDef.AddObject(parsQName)
			appDef.AddCommand(qName).SetParam(parsQName)
			cfg.Resources.Add(istructsmem.NewCommandFunction(qName, istructsmem.NullCommandExec))

			cfg.FunctionRateLimits.AddWorkspaceLimit(qName, istructs.RateLimit{
				Period:                time.Minute,
				MaxAllowedPerDuration: 2,
			})
		})
	defer tearDown(app)

	request := ibus.Request{
		Body:     []byte(`{"args":{}}`),
		AppQName: istructs.AppQName_untill_airs_bp.String(),
		WSID:     1,
		Resource: "c.sys.MyCmd",
		Header:   app.sysAuthHeader,
	}

	// first 2 calls are ok
	for i := 0; i < 2; i++ {
		resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, request, coreutils.GetTestBustTimeout())
		require.Nil(err, err)
		require.Nil(secErr, secErr)
		require.Nil(sections)
		require.Equal(http.StatusOK, resp.StatusCode)
	}

	// 3rd exceeds rate limits
	resp, sections, secErr, err := app.bus.SendRequest2(app.ctx, request, coreutils.GetTestBustTimeout())
	require.Nil(err, err)
	require.Nil(secErr, secErr)
	require.Nil(sections)
	require.Equal(http.StatusTooManyRequests, resp.StatusCode)
}

type testApp struct {
	ctx               context.Context
	cfg               *istructsmem.AppConfigType
	bus               ibus.IBus
	cancel            context.CancelFunc
	done              chan struct{}
	cmdProcService    pipeline.IService
	serviceChannel    CommandChannel
	n10nBroker        in10n.IN10nBroker
	n10nBrokerCleanup func()

	appTokens     istructs.IAppTokens
	sysAuthHeader map[string][]string
}

func tearDown(app testApp) {
	// завершим command processor IService
	app.n10nBrokerCleanup()
	app.cancel()
	<-app.done
}

// simulate real app behavior
func replyBadRequest(sender ibus.ISender, message string) {
	res := coreutils.NewHTTPErrorf(http.StatusBadRequest, message)
	sender.SendResponse(ibus.Response{
		ContentType: coreutils.ApplicationJSON,
		StatusCode:  http.StatusBadRequest,
		Data:        []byte(res.ToJSON()),
	})
}

// test app deployment constants
var (
	testAppName                            = istructs.AppQName_untill_airs_bp
	testAppPartsCount                      = 2
	testAppEngines                         = [cluster.ProcessorKind_Count]int{10, 10, 10}
	testAppPartID     istructs.PartitionID = 1
)

func setUp(t *testing.T, prepare func(appDef appdef.IAppDefBuilder, cfg *istructsmem.AppConfigType)) testApp {
	require := require.New(t)
	// command processor - это IService, работающий через CommandChannel(iprocbus.ServiceChannel). Подготовим этот channel
	serviceChannel := make(CommandChannel)
	done := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())

	cfgs := istructsmem.AppConfigsType{}
	asf := mem.Provide()
	appStorageProvider := istorageimpl.Provide(asf)

	// build application
	adb := appdef.New()
	adb.AddObject(istructs.QNameRaw).AddField(processors.Field_RawObject_Body, appdef.DataKind_string, true, appdef.MaxLen(appdef.MaxFieldLength))
	cfg := cfgs.AddConfig(istructs.AppQName_untill_airs_bp, adb)
	if prepare != nil {
		prepare(adb, cfg)
	}

	appDef, err := adb.Build()
	require.NoError(err)

	appStructsProvider := istructsmem.Provide(cfgs, iratesce.TestBucketsFactory,
		payloads.ProvideIAppTokensFactory(itokensjwt.TestTokensJWT()), appStorageProvider)

	// prepare the AppParts to borrow AppStructs
	appParts, appPartsClean, err := appparts.New(appStructsProvider)
	require.NoError(err)
	defer appPartsClean()

	appParts.DeployApp(testAppName, appDef, testAppPartsCount, testAppEngines)
	appParts.DeployAppPartitions(testAppName, []istructs.PartitionID{testAppPartID})

	// command processor работает через ibus.SendResponse -> нам нужна реализация ibus
	bus := ibusmem.Provide(func(ctx context.Context, sender ibus.ISender, request ibus.Request) {
		// сымитируем работу реального приложения при приеме запроса-команды
		cmdQName, err := appdef.ParseQName(request.Resource[2:])
		require.NoError(err)
		appQName, err := istructs.ParseAppQName(request.AppQName)
		require.NoError(err)
		tp := appDef.Type(cmdQName)
		if tp.Kind() == appdef.TypeKind_null {
			replyBadRequest(sender, "unknown function")
			return
		}
		token := ""
		if authHeaders, ok := request.Header[coreutils.Authorization]; ok {
			token = strings.TrimPrefix(authHeaders[0], "Bearer ")
		}
		command := appDef.Command(cmdQName)
		icm := NewCommandMessage(ctx, request.Body, appQName, istructs.WSID(request.WSID), sender, testAppPartID, command, token, "")
		serviceChannel <- icm
	})
	n10nBroker, n10nBrokerCleanup := in10nmem.ProvideEx2(in10n.Quotas{
		Channels:               1000,
		ChannelsPerSubject:     10,
		Subsciptions:           1000,
		SubsciptionsPerSubject: 10,
	}, time.Now)

	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, time.Now)
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(testAppName)
	systemToken, err := payloads.GetSystemPrincipalTokenApp(appTokens)
	require.NoError(err)
	as, err := appStructsProvider.AppStructs(istructs.AppQName_untill_airs_bp)
	require.NoError(err)
	syncActualizerFactory := projectors.ProvideSyncActualizerFactory()
	op := func(vvmCtx context.Context, partitionID istructs.PartitionID) pipeline.ISyncOperator {
		if len(as.SyncProjectors()) == 0 {
			return &pipeline.NOOP{}
		}
		conf := projectors.SyncActualizerConf{
			Ctx: vvmCtx,
			AppStructs: func() istructs.IAppStructs {
				return as
			},
			SecretReader: itokensjwt.ProvideTestSecretsReader(nil),
			Partition:    partitionID,
			WorkToEvent: func(work interface{}) istructs.IPLogEvent {
				return work.(interface{ Event() istructs.IPLogEvent }).Event()
				// 	switch typed := work.(type) {
				// 	case interface{ Event() istructs.IPLogEvent }:
				// 		return typed.Event()
				// 	case istructs.IPLogEvent:
				// 		return typed
				// 	}
				// 	panic("")
			},
			IntentsLimit: 1,
			N10nFunc:     nil,
		}
		return syncActualizerFactory(conf, as.SyncProjectors()[0], as.SyncProjectors()[1:]...)
	}
	cmdProcessorFactory := ProvideServiceFactory(appParts, time.Now, op, n10nBroker, imetrics.Provide(), "vvm", iauthnzimpl.NewDefaultAuthenticator(iauthnzimpl.TestSubjectRolesGetter), iauthnzimpl.NewDefaultAuthorizer(), isecretsimpl.ProvideSecretReader())
	cmdProcService := cmdProcessorFactory(serviceChannel, testAppPartID)

	go func() {
		cmdProcService.Run(ctx)
		close(done)
	}()

	// skip checking workspace initialization
	coreutils.AddDummyWS(1)
	coreutils.AddDummyWS(2)

	return testApp{
		cfg:               cfg,
		bus:               bus,
		cancel:            cancel,
		ctx:               ctx,
		done:              done,
		cmdProcService:    cmdProcService,
		serviceChannel:    serviceChannel,
		n10nBroker:        n10nBroker,
		n10nBrokerCleanup: n10nBrokerCleanup,
		appTokens:         appTokens,
		sysAuthHeader:     getAuthHeader(systemToken),
	}
}

func jsonEscape(i string) string {
	b, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}
	s := string(b)
	return s[1 : len(s)-1]
}
