package executor

import (
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"testing"
	"time"
)

type xAction struct {
	name    string
	version int
	flag    bool
}

func (x *xAction) cleanUp() {
	x.flag = !x.flag
	x.version = x.version + 1
}

func (x *xAction) Copy() *xAction {
	return &xAction{
		name:    x.name,
		version: x.version,
		flag:    x.flag,
	}
}

func TestMoveHelper_lockCallBackSimple(t *testing.T) {
	ttl := time.Second * 2
	store := util.NewExpirationMap(ttl)
	stop := make(chan struct{})
	go store.Run(stop)
	defer close(stop)

	a := &xAction{
		name:    "hello",
		version: 1,
		flag:    false,
	}
	aa := a.Copy()

	b := &xAction{
		name:    "world",
		version: 100,
		flag:    true,
	}
	bb := b.Copy()

	_, ok1 := store.Add(a.name, nil, func(obj interface{}) {
		a.cleanUp()
	})
	_, ok2 := store.Add(b.name, nil, func(obj interface{}) {
		b.cleanUp()
	})

	if !ok1 || !ok2 {
		t.Error("Add expirationMap failed.")
	}

	//wait for keys to expire
	time.Sleep(ttl * 2)
	if store.Size() != 0 {
		t.Error("ExpirationMap expire failed.")
	}

	if a.version != aa.version+1 || a.flag != !aa.flag {
		t.Errorf("callBackTest failed [%v Vs. %v], [%v Vs. %v].", a.version, aa.version+1, a.flag, !aa.flag)
	}

	if b.version != bb.version+1 || b.flag != !bb.flag {
		t.Errorf("callBackTest failed [%v Vs. %v], [%v Vs. %v].", b.version, bb.version+1, b.flag, !bb.flag)
	}
}
