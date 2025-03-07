package group

import (
	"fmt"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type SettingPolicybuilder struct {
	name        string
	displayName string
	settings    []*proto.GroupDTO_Setting
	ec          *builder.ErrorCollector
}

func NewSettingPolicyBuilder() *SettingPolicybuilder {
	return &SettingPolicybuilder{}
}

func (spb *SettingPolicybuilder) WithName(name string) *SettingPolicybuilder {
	spb.name = name
	return spb
}

func (spb *SettingPolicybuilder) WithDisplayName(displayName string) *SettingPolicybuilder {
	spb.displayName = displayName
	return spb
}

func (spb *SettingPolicybuilder) WithSettings(settings []*proto.GroupDTO_Setting) *SettingPolicybuilder {
	spb.settings = settings
	return spb
}

func (spb *SettingPolicybuilder) Build() (*proto.GroupDTO_SettingPolicy_, error) {
	settingsDto := &proto.GroupDTO_SettingPolicy_{
		SettingPolicy: &proto.GroupDTO_SettingPolicy{},
	}

	if spb.name == "" {
		spb.ec.Collect(fmt.Errorf("Name is required"))
	}
	if spb.displayName == "" {
		spb.ec.Collect(fmt.Errorf("DisplayName is required"))
	}

	if spb.ec.Count() > 0 {
		return nil, fmt.Errorf("error building settings policy: %s", spb.ec.Error())
	}
	settingsDto.SettingPolicy.Name = &spb.name
	settingsDto.SettingPolicy.DisplayName = &spb.displayName
	settingsDto.SettingPolicy.Settings = spb.settings

	return settingsDto, nil
}

type SettingsBuilder struct {
	Settings []*proto.GroupDTO_Setting
}

func NewSettingsBuilder() *SettingsBuilder {
	return &SettingsBuilder{}
}

func (sb *SettingsBuilder) AddSetting(setting *proto.GroupDTO_Setting) *SettingsBuilder {
	sb.Settings = append(sb.Settings, setting)
	return sb
}

func (sb *SettingsBuilder) Build() []*proto.GroupDTO_Setting {
	return sb.Settings
}

func NewResizeAutomationPolicySetting(actionCapability string) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_RESIZE_AUTOMATION_MODE.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
			StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
				Value: &actionCapability,
			},
		},
	}
}

func NewHorizontalScaleUpAutomationPolicySetting(actionMode string) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_HORIZONTAL_SCALE_UP_AUTOMATION_MODE.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
			StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
				Value: &actionMode,
			},
		},
	}
}

func NewHorizontalScaleDownAutomationPolicySetting(actionMode string) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_HORIZONTAL_SCALE_DOWN_AUTOMATION_MODE.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
			StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
				Value: &actionMode,
			},
		},
	}
}

func NewMoveAutomationPolicySetting(actionMode string) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_MOVE_AUTOMATION_MODE.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
			StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
				Value: &actionMode,
			},
		},
	}
}

func NewMinReplicasPolicySetting(minReplicas float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_MIN_REPLICAS.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &minReplicas,
			},
		},
	}
}

func NewMaxReplicasPolicySetting(maxReplicas float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_MAX_REPLICAS.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &maxReplicas,
			},
		},
	}
}

func NewResponseTimeSLOPolicySetting(responseTimeSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_RESPONSE_TIME_SLO.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &responseTimeSLO,
			},
		},
	}
}

func NewServiceTimeSLOPolicySetting(serviceTimeSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_SERVICE_TIME_SLO.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &serviceTimeSLO,
			},
		},
	}
}

func NewQueuingTimeSLOPolicySetting(queuingTimeSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_QUEUING_TIME_SLO.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &queuingTimeSLO,
			},
		},
	}
}

func NewConcurrentQueriesSLOPolicySetting(concurrentQueriesSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_CONCURRENT_QUERIES_SLO.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &concurrentQueriesSLO,
			},
		},
	}
}

func NewTransactionSLOPolicySetting(transactionSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_TRANSACTION_SLO.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &transactionSLO,
			},
		},
	}
}

func NewLlmCacheSLOPolicySetting(llmCacheSLO float32) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_LLM_CACHE_SLO.Enum().Enum(),
		SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
			NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
				Value: &llmCacheSLO,
			},
		},
	}
}

func NewVirtualMachineSuspendAutomationPolicySettings(actionMode string) *proto.GroupDTO_Setting {
	return &proto.GroupDTO_Setting{
		Type: proto.GroupDTO_Setting_VIRTUAL_MACHINE_SUSPEND_AUTOMATION_MODE.Enum(),
		SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
			StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
				Value: &actionMode,
			},
		},
	}
}

func NewPolicySetting(settingType proto.GroupDTO_Setting_SettingType, value interface{}) *proto.GroupDTO_Setting {
	switch v := value.(type) {
	case string:
		return &proto.GroupDTO_Setting{
			Type: settingType.Enum(),
			SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
				StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
					Value: &v,
				},
			},
		}
	case bool:
		return &proto.GroupDTO_Setting{
			Type: settingType.Enum(),
			SettingValueType: &proto.GroupDTO_Setting_BooleanSettingValueType_{
				BooleanSettingValueType: &proto.GroupDTO_Setting_BooleanSettingValueType{
					Value: &v,
				},
			},
		}
	case float32:
		return &proto.GroupDTO_Setting{
			Type: settingType.Enum(),
			SettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType_{
				NumericSettingValueType: &proto.GroupDTO_Setting_NumericSettingValueType{
					Value: &v,
				},
			},
		}
	default:
		panic(fmt.Sprintf("unsupported value type: %T", value))
	}
}
