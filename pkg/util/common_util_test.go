package util

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"testing"
	"time"
)

var (
	testObj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"component": "api",
			"containers": []map[string]interface{}{
				{
					"name":     "foo",
					"resource": "memory",
				},
				{
					"name": "bar",
					"resource": map[string]map[string]string{
						"limits": {
							"memory": "4Gi",
						},
					},
				},
			},
		},
	}
)

func TestCompareVersion(t *testing.T) {

	v1 := "1.4.9.0"
	v2 := "1.5"

	if flag := CompareVersion(v1, v2); flag >= 0 {
		t.Errorf("wrong [-1 Vs %d] for v1=%s, v2=%s", flag, v1, v2)
	}

	if flag := CompareVersion(v2, v1); flag < 0 {
		t.Errorf("wrong [1 Vs %d] for v1=%s, v2=%s", flag, v1, v2)
	}

	v3 := "1.5.0.0"
	if flag := CompareVersion(v2, v3); flag != 0 {
		t.Errorf("wrong [0 Vs %d] for v1=%s, v2=%s", flag, v1, v2)
	}
}

func TestCompareVersion2(t *testing.T) {

	type testCase struct {
		v1   string
		v2   string
		flag int
	}

	alist := []*testCase{
		&testCase{
			v1:   "1.3.2",
			v2:   "1.3.1",
			flag: 1,
		},

		&testCase{
			v1:   "1.0",
			v2:   "1.0.1",
			flag: -1,
		},

		&testCase{
			v1:   "1.a",
			v2:   "1.0",
			flag: 0,
		},
	}

	for _, c := range alist {
		result := CompareVersion(c.v1, c.v2)
		if result != c.flag {
			t.Errorf("Test compareVersion failed: [%d Vs. %d], v1=%s, v2=%s", result, c.flag, c.v1, c.v2)
		}
	}
}

func TestRetryDuring_1(t *testing.T) {

	a := 1
	b := a
	err := RetryDuring(1, time.Second*10, time.Second, func() error {
		a = a + 1
		return nil
	})

	if err != nil {
		t.Errorf("RetryDuring test failed. wrong return.")
	}

	if a != b+1 {
		t.Errorf("RetryDuring test failed [%v Vs. %v]", a, b+1)
	}
}

func TestRetryDuring_2(t *testing.T) {

	a := 1
	b := a
	n := 2
	err := RetryDuring(n+1, time.Second*10, time.Second, func() error {
		a = a + 1
		if a < b+n {
			return fmt.Errorf("not enough")
		}
		return nil
	})

	if err != nil {
		t.Errorf("RetryDuring test failed. wrong return.")
	}

	if a != b+n {
		t.Errorf("RetryDuring test failed [%v Vs. %v]", a, b+n)
	}
}

func TestRetryDuring_RetryCount(t *testing.T) {

	a := 1
	b := a
	n := 2
	err := RetryDuring(1, time.Second*10, time.Second, func() error {
		a = a + 1
		if a < b+n {
			return fmt.Errorf("not enough")
		}
		return nil
	})

	if err == nil {
		t.Errorf("RetryDuring test failed. wrong return.")
	}

	if a != b+1 {
		t.Errorf("RetryDuring test failed [%v Vs. %v]", a, b+1)
	}
}

func TestRetryDuring_Timeout(t *testing.T) {

	a := 1
	b := a
	n := 2
	timeout := time.Second * 2
	err := RetryDuring(100, timeout, time.Second, func() error {
		a = a + 1
		time.Sleep(timeout)
		if a < b+n {
			return fmt.Errorf("not enough")
		}
		return nil
	})

	if err == nil {
		t.Errorf("RetryDuring test failed. wrong return.")
	}

	if a != b+1 {
		t.Errorf("RetryDuring test failed [%v Vs. %v]", a, b+1)
	}
}

func TestNestedField(t *testing.T) {
	type args struct {
		obj  *unstructured.Unstructured
		name string
		path string
	}
	tests := []struct {
		args      args
		value     interface{}
		found     bool
		expectErr bool
	}{
		{
			args: args{
				obj:  testObj,
				name: "test1",
				path: ".component",
			},
			value:     "api",
			found:     true,
			expectErr: false,
		},
		{
			args: args{
				obj:  testObj,
				name: "test2",
				path: ".containers[?(@.name=='bar')].resource",
			},
			value: map[string]map[string]string{
				"limits": {
					"memory": "4Gi",
				},
			},
			found:     true,
			expectErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.args.name, func(t *testing.T) {
			value, found, err := NestedField(testObj, test.args.name, test.args.path)
			if (err != nil) != test.expectErr {
				t.Errorf("NestedField() error = %v, expectErr %v", err, test.expectErr)
				return
			}
			if !reflect.DeepEqual(value, test.value) {
				t.Errorf("NestedField() value = %v, value %v", value, test.value)
			}
			if found != test.found {
				t.Errorf("NestedField() found = %v, value %v", found, test.found)
			}
		})
	}
}

func TestNestedField_NotFound(t *testing.T) {
	path := ".containers[?(@.name=='baz')].resource"
	value, found, err := NestedField(testObj, "test", path)
	assert.Nil(t, err)
	assert.False(t, found)
	assert.Nil(t, value)
}

func TestNestedField_ExpectErr(t *testing.T) {
	path := "component"
	value, found, err := NestedField(testObj, "test", path)
	assert.NotNil(t, err)
	assert.False(t, found)
	assert.Nil(t, value)
}
