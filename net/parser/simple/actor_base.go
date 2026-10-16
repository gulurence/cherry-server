package simple

import (
	cfacade "github.com/gulurence/cherry-server/facade"
	clog "github.com/gulurence/cherry-server/logger"
	cactor "github.com/gulurence/cherry-server/net/actor"
	cproto "github.com/gulurence/cherry-server/net/proto"
)

// ActorBase provides convenience methods for actors to respond to clients.
type ActorBase struct {
	cactor.Base
}

// Response sends a response payload back to the client for the given session and message id.
func (p *ActorBase) Response(session *cproto.Session, mid uint32, v interface{}) {
	Response(p, session, mid, v)
}

// Response looks up the agent by the session and sends a response payload back to the client.
func Response(iActor cfacade.IActor, session *cproto.Session, mid uint32, v interface{}) {
	data, err := iActor.App().Serializer().Marshal(v)
	if err != nil {
		clog.Warnf("[Response] Marshal error. v = %+v", v)
		return
	}

	rsp := &cproto.PomeloResponse{
		Sid:  session.Sid,
		Mid:  mid,
		Data: data,
	}

	iActor.Call(session.AgentPath, ResponseFuncName, rsp)
}
