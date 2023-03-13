package worker

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/golang/glog"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-policy/api/v1alpha1"
)

const (
	SLOHorizontalScale     = "SLOHorizontalScale"
	ContainerVerticalScale = "ContainerVerticalScale"
	Service                = "Service"
	Deployment             = "Deployment"
)

var validKinds = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
}

func validWorkloadController(kind string) bool {
	return validKinds[kind]
}

// validateActionMode validates the action mode
// Valid action mode must exist in actionModes map
func validateActionMode(actionMode v1alpha1.ActionMode) (string, error) {
	switch actionMode {
	case v1alpha1.Automatic:
		return "AUTOMATIC", nil
	case v1alpha1.Manual:
		return "MANUAL", nil
	case v1alpha1.Recommend:
		return "RECOMMEND", nil
	case v1alpha1.Disabled:
		return "DISABLED", nil
	default:
		return "", fmt.Errorf("unsupported action mode %v", actionMode)
	}
}

func addCVSLimitSettings(prefix string, qty *v1alpha1.LimitResourceConstraint, settings *group.SettingsBuilder) error {
	if qty == nil {
		return nil
	}
	isCpu := strings.Contains(prefix, "CPU")
	typeMap := proto.GroupDTO_Setting_SettingType_value
	if max := qty.Max; max != nil {
		val, error := QuantityValue(isCpu, max)
		if error != nil {
			return error
		}

		glog.V(4).Infof("Limit_MAX ", prefix, val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MAX"]), val))
	}

	if min := qty.Min; min != nil {
		val, error := QuantityValue(isCpu, min)
		if error != nil {
			return error
		}
		glog.V(4).Infof("Limit_MIN ", prefix, val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MIN"]), val))
	}

	if aboveMax := qty.RecommendAboveMax; aboveMax != nil {
		glog.V(4).Infof("Limit_aboveMax ", prefix, *aboveMax)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_ABOVE_MAX"]), *aboveMax))
	}

	if belowMin := qty.RecommendBelowMin; belowMin != nil {
		glog.V(4).Infof("Limit_aboveMax ", prefix, *belowMin)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_BELOW_MIN"]), *belowMin))
	}
	return nil
}

func addCVSRequestSettings(prefix string, qty *v1alpha1.RequestResourceConstraint, settings *group.SettingsBuilder) error {
	if qty == nil {
		return nil
	}
	typeMap := proto.GroupDTO_Setting_SettingType_value

	isCpu := strings.Contains(prefix, "CPU")
	if min := qty.Min; min != nil {
		val, error := QuantityValue(isCpu, min)
		if error != nil {
			return error
		}
		glog.V(4).Infof("Increments.REQUEST_MIN ", prefix, val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MIN"]), val))
	}

	if belowMin := qty.RecommendBelowMin; belowMin != nil {
		glog.V(4).Infof("Increments.REQUEST_aboveMax ", prefix,  *belowMin)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_BELOW_MIN"]), *belowMin))
	}
	return nil
}

var MinObservationPeriod = map[string]float32{
	"1d": 1.0,
	"3d": 3.0,
	"7d": 7.0,
}

var MaxObservationPeriod = map[string]float32{
	"7d":  7.0,
	"30d": 30.0,
	"90d": 90.0,
}

var PercentileAggressiveness = map[string]float32{
	"p90":   90.0,
	"p95":   95.0,
	"p99":   99.0,
	"p99_1": 99.1,
	"p99_5": 99.5,
	"p99_9": 99.9,
	"p100":  100.0,
}

var VcpuIncrement = map[string]float32{
	"Deployment":  90.0,
	"StatefulSet": 100.0,
	"DaemonSet":   99.0,
}

var VmemIncrement = map[string]float32{
	"Deployment":  90.0,
	"StatefulSet": 100.0,
	"DaemonSet":   99.0,
}

var RateOfResize = map[string]float32{
	"high":   3.0,
	"medium": 2.0,
	"low":    1.0,
}

func QuantityToMilliCore(q *resource.Quantity) (float32, error) {
	if q.Format != resource.DecimalSI {
		return -1.0, fmt.Errorf("invalid quantity format: %v (must be DecimalSI)", q.Format)
	}

	milliCore := q.MilliValue()
	if milliCore < 0 {
		return -1.0, fmt.Errorf("quantity is negative")
	}

	return float32(milliCore), nil
}

func QuantityToMB(q *resource.Quantity) (float32, error) {
	mb := q.AsApproximateFloat64() / 1000000
	if mb < 0 {
		return -1.0, fmt.Errorf("quantity is negative")
	}
	return float32(mb), nil
}

func QuantityValue(isCPU bool, q *resource.Quantity) (float32, error) {
	if isCPU {
		return QuantityToMilliCore(q)
	}
	return QuantityToMB(q)
}

func parseFloatFromPercentString(s string) (float32, error) {
	trimmed := strings.TrimSuffix(s, "%")
	value, err := strconv.ParseFloat(trimmed, 32)
	if err != nil {
		return 0, err
	}
	return float32(value), nil
}
