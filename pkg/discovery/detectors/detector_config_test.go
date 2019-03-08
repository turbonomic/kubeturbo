package detectors

import (
	"encoding/json"
	"fmt"
	"testing"
)

const validFullConfig = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        },
        "masterNodeDetectors": {
           "nodeNamePatterns": [ ".*master.*" ],
           "nodeLabels": [ {"key": "node-role.kubernetes.io/master", "value": ".*", "master" : "yes"} ]
        },
        "daemonPodDetectors": {
           "namespaces": [ "kube-system", "kube-service-catalog", "openshift-.*" ],
           "podNamePatterns": ["monitor-1"]
        }
    }
`

const missingMasterNodeDetectors = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        },
        "daemonPodDetectors": {
           "namespaces": [ "kube-system", "kube-service-catalog", "openshift-.*" ],
           "podNamePatterns": []
        }
    }
`

const missingDaemonPodDetectors = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        },
        "masterNodeDetectors": {
           "nodeNamePatterns": [ ".*master.*" ],
           "nodeLabels": [ {"key": "node-role.kubernetes.io/master", "value": ".*"} ]
        }
    }
`

// This is the configuration as it stood before adding detectors
const missingBoth = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        }
    }
`

const missingList = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        },
        "masterNodeDetectors": {
           "nodeLabels": [ {"key": "node-role.kubernetes.io/master", "value": ".*"} ]
        },
        "daemonPodDetectors": {
           "namespaces": [ "kube-system", "kube-service-catalog", "openshift-.*" ],
           "podNamePatterns": []
        }
    }
`

const emptySection = `    {
        "communicationConfig": {
            "serverMeta": {
                "version": "6.3.0-SNAPSHOT",
                "turboServer": "https://167.99.235.136:9400"
            },
            "restAPIConfig": {
                "opsManagerUserName": "administrator",
                "opsManagerPassword": "a"
            }
        },
        "targetConfig": {
            "targetName":"ibm"
        },
        "masterNodeDetectors": {
           "nodeNamePatterns": [ ".*master.*" ],
           "nodeLabels": [ {"key": "node-role.kubernetes.io/master", "value": ".*"} ]
        },
        "daemonPodDetectors": {
        }
    }
`

var configs = map[string]string{
	"validFullConfig":            validFullConfig,
	"missingMasterNodeDetectors": missingMasterNodeDetectors,
	"missingDaemonPodDetectors":  missingDaemonPodDetectors,
	"missingBoth":                missingBoth,
	"missingList":                missingList,
	"emptySection":               emptySection,
}

/*
 * Stub out unneeded fields of K8sTAPServiceSpec in order to avoid import cycles
 */
type X1 interface{}
type X2 interface{}

type TestConfig struct {
	X1                   // Stubbed service.TurboCommunicationConfig
	X2                   // Stubbed configs.K8sTargetConfig
	*MasterNodeDetectors `json:"masterNodeDetectors,omitempty"`
	*DaemonPodDetectors  `json:"daemonPodDetectors,omitempty"`
}

func loadConfig(config string) (*TestConfig, error) {
	var spec TestConfig
	err := json.Unmarshal([]byte(config), &spec)
	if err != nil {
		return nil, fmt.Errorf("Unmarshall error :%v", err.Error())
	}
	//fmt.Println(spec)
	return &spec, nil
}

func setupTest(t *testing.T, scenario string) (*TestConfig, error) {
	spec, err := loadConfig(configs[scenario])
	if err != nil {
		return nil, err
	}
	return spec, ValidateAndParseDetectors(spec.MasterNodeDetectors, spec.DaemonPodDetectors)
}

func TestDetectorConfig_loadConfig(t *testing.T) {
	for name, _ := range configs {
		_, err := setupTest(t, name)
		if err != nil {
			t.Errorf("Cannot parse configuration '%s': %v", name, err)
		}
	}
}

/*
 * Master Node Test cases:
 * - no label
 * - one matching label, with and without matching value
 * - one non-matching label
 * - matching name (no labels)
 * - non-matching name (no labels)
 *
 * Results is an array of expected results for each of the following
 * configuration scenarios:
 *
 *   - validFullConfig
 *   - missingMasterNodeDetectors
 *   - missingDaemonPodDetectors
 *   - missingBoth
 *   - missingList
 *   - emptySection
 */

type MasterNodeTestCase struct {
	Name    string
	Labels  map[string]string
	Results []bool
}

var masterNodeTests = []MasterNodeTestCase{
	{"n1", map[string]string{},
		[]bool{false, false, false, false, false, false}},
	{"n2", map[string]string{"master": "yes"},
		[]bool{false, false, false, false, false, false}},
	{"n3", map[string]string{"master": "no"},
		[]bool{false, false, false, false, false, false}},
	{"n4", map[string]string{"node-role.kubernetes.io/master": "foo"},
		[]bool{true, false, true, false, true, true}},
	{"master-node", map[string]string{},
		[]bool{true, false, true, false, false, true}},
	{"worker-node", map[string]string{},
		[]bool{false, false, false, false, false, false}},
}

func TestDetectorConfig_detectMasterNode(t *testing.T) {
	for scenarioIndex, scenarioName := range []string{"validFullConfig", "missingMasterNodeDetectors",
		"missingDaemonPodDetectors", "missingBoth", "missingList", "emptySection"} {
		setupTest(t, scenarioName)
		for _, test := range masterNodeTests {
			is := IsMasterDetected(test.Name, test.Labels)
			expected := test.Results[scenarioIndex]
			if is != expected {
				t.Errorf("Config scenario '%s' IsMasterDetected(\"%s\", %v) returned '%v', expected '%v'\n",
					scenarioName, test.Name, test.Labels, is, expected)
			}
			//fmt.Printf("Scenario %s.%d: isMaster=%v, expected %v\n", scenarioName, scenarioIndex,
			//	IsMasterDetected(test.Name, test.Labels), test.Results[scenarioIndex])
		}
	}
}

/*
 * Daemon Pod test cases
 */
type DaemonPodTestCase struct {
	Name      string
	Namespace string
	Results   []bool
}

var daemonPodTests = []DaemonPodTestCase{
	{"p1", "", []bool{false, false, true, true, false, true}},
	{"p2", "openshift-test", []bool{true, true, false, false, true, false}},
	{"monitor-1", "unlrelated-ns", []bool{true, false, false, false, false, false}},
}

func TestDetectorConfig_detectDaemon(t *testing.T) {
	for scenarioIndex, scenarioName := range []string{"validFullConfig", "missingMasterNodeDetectors",
		"missingDaemonPodDetectors", "missingBoth", "missingList", "emptySection"} {
		setupTest(t, scenarioName)
		for _, test := range daemonPodTests {
			is := IsDaemonDetected(test.Name, test.Namespace)
			expected := test.Results[scenarioIndex]
			if is != expected {
				t.Errorf("Config scenario '%s' IsMasterDetected(\"%s\", \"%s\") returned '%v', expected '%v'\n",
					scenarioName, test.Name, test.Namespace, is, expected)
			}
			//fmt.Printf("Scenario %s.%d: isMaster=%v, expected %v\n", scenarioName, scenarioIndex,
			//	IsDaemonDetected(test.Name, test.Namespace), test.Results[scenarioIndex])
		}
	}
}
