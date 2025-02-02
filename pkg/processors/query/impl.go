/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 *
 * * @author Michael Saigachenko
 */

package queryprocessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/iprocbus"
	"github.com/voedger/voedger/pkg/isecrets"
	"github.com/voedger/voedger/pkg/isecretsimpl"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	imetrics "github.com/voedger/voedger/pkg/metrics"
	"github.com/voedger/voedger/pkg/pipeline"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys/authnz"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"

	coreutils "github.com/voedger/voedger/pkg/utils"
)

func implRowsProcessorFactory(ctx context.Context, appDef appdef.IAppDef, state istructs.IState, params IQueryParams,
	resultMeta appdef.IType, rs IResultSenderClosable, metrics IMetrics) pipeline.IAsyncPipeline {
	operators := make([]*pipeline.WiredOperator, 0)
	if resultMeta == nil {
		// happens when the query has no result, e.g. q.air.UpdateSubscriptionDetails
		operators = append(operators, pipeline.WireAsyncOperator("noop, no result", &pipeline.AsyncNOOP{}))
	} else if resultMeta.QName() == istructs.QNameRaw {
		operators = append(operators, pipeline.WireAsyncOperator("Raw result", &RawResultOperator{
			metrics: metrics,
		}))
	} else {
		fieldsDefs := &fieldsDefs{
			appDef: appDef,
			fields: make(map[appdef.QName]FieldsKinds),
		}
		rootFields := newFieldsKinds(resultMeta)
		operators = append(operators, pipeline.WireAsyncOperator("Result fields", &ResultFieldsOperator{
			elements:   params.Elements(),
			rootFields: rootFields,
			fieldsDefs: fieldsDefs,
			metrics:    metrics,
		}))
		operators = append(operators, pipeline.WireAsyncOperator("Enrichment", &EnrichmentOperator{
			state:      state,
			elements:   params.Elements(),
			fieldsDefs: fieldsDefs,
			metrics:    metrics,
		}))
		if len(params.Filters()) != 0 {
			operators = append(operators, pipeline.WireAsyncOperator("Filter", &FilterOperator{
				filters:    params.Filters(),
				rootFields: rootFields,
				metrics:    metrics,
			}))
		}
		if len(params.OrderBy()) != 0 {
			operators = append(operators, pipeline.WireAsyncOperator("Order", newOrderOperator(params.OrderBy(), metrics)))
		}
		if params.StartFrom() != 0 || params.Count() != 0 {
			operators = append(operators, pipeline.WireAsyncOperator("Counter", newCounterOperator(
				params.StartFrom(),
				params.Count(),
				metrics)))
		}
	}
	operators = append(operators, pipeline.WireAsyncOperator("Send to bus", &SendToBusOperator{
		rs:      rs,
		metrics: metrics,
	}))
	return pipeline.NewAsyncPipeline(ctx, "Rows processor", operators[0], operators[1:]...)
}

func implServiceFactory(serviceChannel iprocbus.ServiceChannel, resultSenderClosableFactory ResultSenderClosableFactory,
	appParts appparts.IAppPartitions, maxPrepareQueries int, metrics imetrics.IMetrics, vvm string,
	authn iauthnz.IAuthenticator, authz iauthnz.IAuthorizer) pipeline.IService {
	secretReader := isecretsimpl.ProvideSecretReader()
	return pipeline.NewService(func(ctx context.Context) {
		var p pipeline.ISyncPipeline
		for ctx.Err() == nil {
			select {
			case intf := <-serviceChannel:
				now := time.Now()
				msg := intf.(IQueryMessage)
				qpm := &queryProcessorMetrics{
					vvm:     vvm,
					app:     msg.AppQName(),
					metrics: metrics,
				}
				qpm.Increase(queriesTotal, 1.0)
				rs := resultSenderClosableFactory(msg.RequestCtx(), msg.Sender())
				qwork := newQueryWork(msg, rs, appParts, maxPrepareQueries, qpm, secretReader)
				if p == nil {
					p = newQueryProcessorPipeline(ctx, authn, authz)
				}
				err := p.SendSync(qwork)
				if err != nil {
					qpm.Increase(errorsTotal, 1.0)
					p.Close()
					p = nil
				} else {
					err = execAndSendResponse(ctx, qwork)
				}
				if qwork.rowsProcessor != nil {
					// wait until all rows are sent
					qwork.rowsProcessor.Close()
				}
				err = coreutils.WrapSysError(err, http.StatusInternalServerError)
				rs.Close(err)
				qwork.release()
				metrics.IncreaseApp(queriesSeconds, vvm, msg.AppQName(), time.Since(now).Seconds())
			case <-ctx.Done():
			}
		}
		if p != nil {
			p.Close()
		}
	})
}

func execAndSendResponse(ctx context.Context, qw *queryWork) (err error) {
	now := time.Now()
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			err = fmt.Errorf("%v\n%s", r, stack)
		}
		qw.metrics.Increase(execSeconds, time.Since(now).Seconds())
	}()
	return qw.queryExec(ctx, qw.execQueryArgs, func(object istructs.IObject) error {
		pathToIdx := make(map[string]int)
		if qw.resultType.QName() == istructs.QNameRaw {
			pathToIdx[processors.Field_RawObject_Body] = 0
		} else {
			for i, element := range qw.queryParams.Elements() {
				pathToIdx[element.Path().Name()] = i
			}
		}
		return qw.rowsProcessor.SendAsync(rowsWorkpiece{
			object: object,
			outputRow: &outputRow{
				keyToIdx: pathToIdx,
				values:   make([]interface{}, len(pathToIdx)),
			},
			enrichedRootFieldsKinds: make(map[string]appdef.DataKind),
		})
	})
}

func newQueryProcessorPipeline(requestCtx context.Context, authn iauthnz.IAuthenticator, authz iauthnz.IAuthorizer) pipeline.ISyncPipeline {
	ops := []*pipeline.WiredOperator{
		operator("borrowAppPart", borrowAppPart),
		operator("check function call rate", func(ctx context.Context, qw *queryWork) (err error) {
			if qw.appStructs.IsFunctionRateLimitsExceeded(qw.msg.Query().QName(), qw.msg.WSID()) {
				return coreutils.NewSysError(http.StatusTooManyRequests)
			}
			return nil
		}),
		operator("authenticate query request", func(ctx context.Context, qw *queryWork) (err error) {
			req := iauthnz.AuthnRequest{
				Host:        qw.msg.Host(),
				RequestWSID: qw.msg.WSID(),
				Token:       qw.msg.Token(),
			}
			if qw.principals, qw.principalPayload, err = authn.Authenticate(qw.msg.RequestCtx(), qw.appStructs, qw.appStructs.AppTokens(), req); err != nil {
				return coreutils.WrapSysError(err, http.StatusUnauthorized)
			}
			return
		}),
		operator("check workspace active", func(ctx context.Context, qw *queryWork) (err error) {
			for _, prn := range qw.principals {
				if prn.Kind == iauthnz.PrincipalKind_Role && prn.QName == iauthnz.QNameRoleSystem && prn.WSID == qw.msg.WSID() {
					// system -> allow to work in any case
					return nil
				}
			}

			wsDesc, err := qw.appStructs.Records().GetSingleton(qw.msg.WSID(), authnz.QNameCDocWorkspaceDescriptor)
			if err != nil {
				// notest
				return err
			}
			if wsDesc.QName() == appdef.NullQName {
				// TODO: query prcessor currently does not check workspace initialization
				return nil
			}
			if wsDesc.AsInt32(authnz.Field_Status) != int32(authnz.WorkspaceStatus_Active) {
				return processors.ErrWSInactive
			}
			return nil
		}),
		operator("authorize query request", func(ctx context.Context, qw *queryWork) (err error) {
			req := iauthnz.AuthzRequest{
				OperationKind: iauthnz.OperationKind_EXECUTE,
				Resource:      qw.msg.Query().QName(),
			}
			ok, err := authz.Authorize(qw.appStructs, qw.principals, req)
			if err != nil {
				return err
			}
			if !ok {
				return coreutils.WrapSysError(errors.New(""), http.StatusForbidden)
			}
			return nil
		}),
		operator("unmarshal request", func(ctx context.Context, qw *queryWork) (err error) {
			parsType := qw.msg.Query().Param()
			if parsType != nil && parsType.QName() == istructs.QNameRaw {
				qw.requestData["args"] = map[string]interface{}{
					processors.Field_RawObject_Body: string(qw.msg.Body()),
				}
				return nil
			}
			err = json.Unmarshal(qw.msg.Body(), &qw.requestData)
			return coreutils.WrapSysError(err, http.StatusBadRequest)
		}),
		operator("validate: get exec query args", func(ctx context.Context, qw *queryWork) (err error) {
			qw.execQueryArgs, err = newExecQueryArgs(qw.requestData, qw.msg.WSID(), qw)
			return coreutils.WrapSysError(err, http.StatusBadRequest)
		}),
		operator("create state", func(ctx context.Context, qw *queryWork) (err error) {
			qw.state = state.ProvideQueryProcessorStateFactory()(
				qw.msg.RequestCtx(),
				qw.appStructs,
				state.SimplePartitionIDFunc(qw.msg.Partition()),
				state.SimpleWSIDFunc(qw.msg.WSID()),
				qw.secretReader,
				func() []iauthnz.Principal { return qw.principals },
				func() string { return qw.msg.Token() })
			qw.execQueryArgs.State = qw.state
			return
		}),
		operator("get queryFunc", func(ctx context.Context, qw *queryWork) (err error) {
			qw.queryFunc = qw.appStructs.Resources().QueryResource(qw.msg.Query().QName()).(istructs.IQueryFunction)
			return nil
		}),
		operator("validate: get result type", func(ctx context.Context, qw *queryWork) (err error) {
			qw.resultType = qw.msg.Query().Result()
			if qw.resultType == nil {
				return nil
			}
			if qw.resultType.QName() == appdef.QNameANY {
				qNameResultType := qw.queryFunc.ResultType(qw.execQueryArgs.PrepareArgs)
				qw.resultType = qw.appStructs.AppDef().Type(qNameResultType)
			}
			err = errIfFalse(qw.resultType.Kind() != appdef.TypeKind_null, func() error {
				return fmt.Errorf("result type %s: %w", qw.resultType, ErrNotFound)
			})
			return coreutils.WrapSysError(err, http.StatusBadRequest)
		}),
		operator("validate: get query params", func(ctx context.Context, qw *queryWork) (err error) {
			qw.queryParams, err = newQueryParams(qw.requestData, NewElement, NewFilter, NewOrderBy, newFieldsKinds(qw.resultType))
			return coreutils.WrapSysError(err, http.StatusBadRequest)
		}),
		operator("authorize result", func(ctx context.Context, qw *queryWork) (err error) {
			req := iauthnz.AuthzRequest{
				OperationKind: iauthnz.OperationKind_SELECT,
				Resource:      qw.msg.Query().QName(),
			}
			for _, elem := range qw.queryParams.Elements() {
				for _, resultField := range elem.ResultFields() {
					req.Fields = append(req.Fields, resultField.Field())
				}
			}
			if len(req.Fields) == 0 {
				return nil
			}
			ok, err := authz.Authorize(qw.appStructs, qw.principals, req)
			if err != nil {
				return err
			}
			if !ok {
				return coreutils.NewSysError(http.StatusForbidden)
			}
			return nil
		}),
		operator("build rows processor", func(ctx context.Context, qw *queryWork) error {
			now := time.Now()
			defer func() {
				qw.metrics.Increase(buildSeconds, time.Since(now).Seconds())
			}()
			qw.rowsProcessor = ProvideRowsProcessorFactory()(qw.msg.RequestCtx(), qw.appStructs.AppDef(),
				qw.state, qw.queryParams, qw.resultType, qw.rs, qw.metrics)
			return nil
		}),
		operator("get func exec", func(ctx context.Context, qw *queryWork) (err error) {
			qw.queryExec = qw.appStructs.Resources().QueryResource(qw.msg.Query().QName()).(istructs.IQueryFunction).Exec
			return nil
		}),
	}
	return pipeline.NewSyncPipeline(requestCtx, "Query Processor", ops[0], ops[1:]...)
}

type queryWork struct {
	// input
	msg      IQueryMessage
	rs       IResultSenderClosable
	appParts appparts.IAppPartitions
	// work
	requestData       map[string]interface{}
	state             istructs.IState
	queryParams       IQueryParams
	appPart           appparts.IAppPartition
	appStructs        istructs.IAppStructs
	resultType        appdef.IType
	execQueryArgs     istructs.ExecQueryArgs
	maxPrepareQueries int
	rowsProcessor     pipeline.IAsyncPipeline
	metrics           IMetrics
	principals        []iauthnz.Principal
	principalPayload  payloads.PrincipalPayload
	secretReader      isecrets.ISecretReader
	queryFunc         istructs.IQueryFunction
	queryExec         func(ctx context.Context, args istructs.ExecQueryArgs, callback istructs.ExecQueryCallback) error
}

func newQueryWork(msg IQueryMessage, rs IResultSenderClosable, appParts appparts.IAppPartitions,
	maxPrepareQueries int, metrics *queryProcessorMetrics, secretReader isecrets.ISecretReader) *queryWork {
	return &queryWork{
		msg:               msg,
		rs:                rs,
		appParts:          appParts,
		requestData:       make(map[string]interface{}),
		maxPrepareQueries: maxPrepareQueries,
		metrics:           metrics,
		secretReader:      secretReader,
	}
}

// need for q.sys.EnrichPrincipalToken
func (qw *queryWork) GetPrincipals() []iauthnz.Principal {
	return qw.principals
}

// borrows app partition for query
func (qw *queryWork) borrow() (err error) {
	if qw.appPart, err = qw.appParts.Borrow(qw.msg.AppQName(), qw.msg.Partition(), cluster.ProcessorKind_Query); err != nil {
		return err
	}
	qw.appStructs = qw.appPart.AppStructs()
	return nil
}

// releases borrowed app partition
func (qw *queryWork) release() {
	if ap := qw.appPart; ap != nil {
		qw.appStructs = nil
		qw.appPart = nil
		ap.Release()
	}
}

// need or q.sys.EnrichPrincipalToken
func (qw *queryWork) AppQName() istructs.AppQName {
	return qw.msg.AppQName()
}

func borrowAppPart(_ context.Context, qw *queryWork) error {
	switch err := qw.borrow(); {
	case err == nil:
		return nil
	case errors.Is(err, appparts.ErrNotAvailableEngines):
		return coreutils.WrapSysError(err, http.StatusServiceUnavailable)
	default:
		return coreutils.WrapSysError(err, http.StatusBadRequest)
	}
}

func operator(name string, doSync func(ctx context.Context, qw *queryWork) (err error)) *pipeline.WiredOperator {
	return pipeline.WireFunc(name, func(ctx context.Context, work interface{}) (err error) {
		return doSync(ctx, work.(*queryWork))
	})
}

func errIfFalse(cond bool, errIfFalse func() error) error {
	if !cond {
		return errIfFalse()
	}
	return nil
}

type queryMessage struct {
	requestCtx context.Context
	appQName   istructs.AppQName
	wsid       istructs.WSID
	partition  istructs.PartitionID
	sender     ibus.ISender
	body       []byte
	query      appdef.IQuery
	host       string
	token      string
}

func (m queryMessage) AppQName() istructs.AppQName     { return m.appQName }
func (m queryMessage) WSID() istructs.WSID             { return m.wsid }
func (m queryMessage) Sender() ibus.ISender            { return m.sender }
func (m queryMessage) RequestCtx() context.Context     { return m.requestCtx }
func (m queryMessage) Query() appdef.IQuery            { return m.query }
func (m queryMessage) Host() string                    { return m.host }
func (m queryMessage) Token() string                   { return m.token }
func (m queryMessage) Partition() istructs.PartitionID { return m.partition }
func (m queryMessage) Body() []byte {
	if len(m.body) != 0 {
		return m.body
	}
	return []byte("{}")
}

func NewQueryMessage(requestCtx context.Context, appQName istructs.AppQName, partID istructs.PartitionID, wsid istructs.WSID, sender ibus.ISender, body []byte,
	query appdef.IQuery, host string, token string) IQueryMessage {
	return queryMessage{
		appQName:   appQName,
		wsid:       wsid,
		partition:  partID,
		sender:     sender,
		body:       body,
		requestCtx: requestCtx,
		query:      query,
		host:       host,
		token:      token,
	}
}

type rowsWorkpiece struct {
	pipeline.IWorkpiece
	object                  istructs.IObject
	outputRow               IOutputRow
	enrichedRootFieldsKinds FieldsKinds
}

func (w rowsWorkpiece) Object() istructs.IObject             { return w.object }
func (w rowsWorkpiece) OutputRow() IOutputRow                { return w.outputRow }
func (w rowsWorkpiece) EnrichedRootFieldsKinds() FieldsKinds { return w.enrichedRootFieldsKinds }
func (w rowsWorkpiece) PutEnrichedRootFieldKind(name string, kind appdef.DataKind) {
	w.enrichedRootFieldsKinds[name] = kind
}
func (w rowsWorkpiece) Release() {
	//TODO implement it someday
	//Release goes here
}

type outputRow struct {
	keyToIdx map[string]int
	values   []interface{}
}

func (r *outputRow) Set(alias string, value interface{}) { r.values[r.keyToIdx[alias]] = value }
func (r *outputRow) Values() []interface{}               { return r.values }
func (r *outputRow) Value(alias string) interface{}      { return r.values[r.keyToIdx[alias]] }
func (r *outputRow) MarshalJSON() ([]byte, error)        { return json.Marshal(r.values) }

func newExecQueryArgs(data coreutils.MapObject, wsid istructs.WSID, qw *queryWork) (execQueryArgs istructs.ExecQueryArgs, err error) {
	args, _, err := data.AsObject("args")
	if err != nil {
		return execQueryArgs, err
	}
	argsType := qw.msg.Query().Param()
	requestArgs := istructs.NewNullObject()
	if argsType != nil {
		requestArgsBuilder := qw.appStructs.ObjectBuilder(argsType.QName())
		requestArgsBuilder.FillFromJSON(args)
		requestArgs, err = requestArgsBuilder.Build()
		if err != nil {
			return execQueryArgs, err
		}
	}
	return istructs.ExecQueryArgs{
		PrepareArgs: istructs.PrepareArgs{
			ArgumentObject: requestArgs,
			Workspace:      wsid,
			Workpiece:      qw,
		},
	}, nil
}

type path []string

func (p path) IsRoot() bool      { return p[0] == rootDocument }
func (p path) Name() string      { return strings.Join(p, "/") }
func (p path) AsArray() []string { return p }

type element struct {
	path   path
	fields []IResultField
	refs   []IRefField
}

func (e element) NewOutputRow() IOutputRow {
	fields := make([]string, 0)
	for _, field := range e.fields {
		fields = append(fields, field.Field())
	}
	for _, field := range e.refs {
		fields = append(fields, field.Key())
	}
	fieldToIdx := make(map[string]int)
	for j, field := range fields {
		fieldToIdx[field] = j
	}
	return &outputRow{
		keyToIdx: fieldToIdx,
		values:   make([]interface{}, len(fieldToIdx)),
	}
}

func (e element) Path() IPath                  { return e.path }
func (e element) ResultFields() []IResultField { return e.fields }
func (e element) RefFields() []IRefField       { return e.refs }

type fieldsDefs struct {
	appDef appdef.IAppDef
	fields map[appdef.QName]FieldsKinds
	lock   sync.Mutex
}

func newFieldsDefs(appDef appdef.IAppDef) *fieldsDefs {
	return &fieldsDefs{
		appDef: appDef,
		fields: make(map[appdef.QName]FieldsKinds),
	}
}

func (c *fieldsDefs) get(name appdef.QName) FieldsKinds {
	c.lock.Lock()
	defer c.lock.Unlock()
	fd, ok := c.fields[name]
	if !ok {
		fd = newFieldsKinds(c.appDef.Type(name))
		c.fields[name] = fd
	}
	return fd
}

type queryProcessorMetrics struct {
	vvm     string
	app     istructs.AppQName
	metrics imetrics.IMetrics
}

func (m *queryProcessorMetrics) Increase(metricName string, valueDelta float64) {
	m.metrics.IncreaseApp(metricName, m.vvm, m.app, valueDelta)
}

func newFieldsKinds(t appdef.IType) FieldsKinds {
	res := FieldsKinds{}
	if fields, ok := t.(appdef.IFields); ok {
		for _, f := range fields.Fields() {
			res[f.Name()] = f.DataKind()
		}
	}
	return res
}
