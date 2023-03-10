package worker

import (
	"fmt"

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

func addCVSLimitSettings(prefix string, qty *v1alpha1.LimitResourceConstraint, settings *group.SettingsBuilder) {
	if qty == nil {
		return
	}
	typeMap := proto.GroupDTO_Setting_SettingType_value
	if max := qty.Max; max != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MAX"]), max.String()))
	}

	if min := qty.Min; min != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MIN"]), min.String()))
	}

	if aboveMax := qty.RecommendAboveMax; aboveMax != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_ABOVE_MAX"]), *aboveMax))
	}

	if belowMin := qty.RecommendBelowMin; belowMin != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_BELOW_MIN"]), *belowMin))
	}
}

func addCVSRequestSettings(prefix string, qty *v1alpha1.RequestResourceConstraint, settings *group.SettingsBuilder) {
	if qty == nil {
		return
	}
	typeMap := proto.GroupDTO_Setting_SettingType_value

	if min := qty.Min; min != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_MIN"]), min.String()))
	}

	if belowMin := qty.RecommendBelowMin; belowMin != nil {
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_SettingType(typeMap[prefix+"_BELOW_MIN"]), *belowMin))
	}
}
