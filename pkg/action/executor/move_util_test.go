package executor

import (
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"testing"
	"time"

	kclient "k8s.io/client-go/kubernetes"
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

func mockGetScheduler(client *kclient.Clientset, nameSpace, name string) (string, error) {
	return "defaultScheduler", nil
}

func mockUpdateScheduler(client *kclient.Clientset, nameSpace, name, schedulerName string) (string, error) {
	return "defaultScheduler", nil
}

func buildHelper(store *util.ExpirationMap) *moveHelper {
	helper := &moveHelper{
		flag:           false,
		nameSpace:      "ns",
		podName:        "pod",
		kind:           "kind",
		controllerName: "controller",
		schedulerNone:  "nonexxx",
		stop:           make(chan struct{}),

		getSchedulerName:    mockGetScheduler,
		updateSchedulerName: mockUpdateScheduler,
	}

	helper.SetMap(store)
	return helper
}

func TestMoveHelper_CleanUp(t *testing.T) {
	ttl := time.Second * 2
	store := util.NewExpirationMap(ttl)
	stop := make(chan struct{})
	go store.Run(stop)
	defer close(stop)

	helper := buildHelper(store)

	//1. acquire lock
	if success := helper.Acquirelock(); !success {
		t.Error("failed to acquire a lock")
	}
	helper.KeepRenewLock()

	time.Sleep(store.GetTTL() + time.Second*5)

	//2. try to renew lock
	if success := helper.Renewlock(); !success {
		t.Errorf("failed to renew lock.")
	}

	//3. release lock by cleanUP
	helper.CleanUp()

	//4. try to renew lock again, should fail.
	if success := helper.Renewlock(); success {
		t.Error("failed to release lock.")
	}

	if store.Size() > 0 {
		t.Errorf("store should be empty: %d", store.Size())
	}
}

func TestMoveHelper_KeepRenewLock(t *testing.T) {
	ttl := time.Second * 2
	store := util.NewExpirationMap(ttl)
	stop := make(chan struct{})
	go store.Run(stop)
	defer close(stop)

	helper := buildHelper(store)

	//1. acquire lock
	if success := helper.Acquirelock(); !success {
		t.Error("failed to acquire a lock")
	}

	//2. keepRenew it, and test it
	helper.KeepRenewLock()
	time.Sleep(store.GetTTL() + time.Second*5)

	if success := helper.Renewlock(); !success {
		t.Errorf("failed to renew lock.")
	}

	//3. Stop keeping renew, and test it
	helper.StopRenew()
	time.Sleep(store.GetTTL() + time.Second*5)
	if success := helper.Renewlock(); success {
		t.Error("failed to release lock.")
	}

	//4. CleanUp
	helper.CleanUp()
	if store.Size() > 0 {
		t.Errorf("store should be empty: %d", store.Size())
	}
}
