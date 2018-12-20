package repository

// Representation of group objects in the kube cluster.
type PolicyGroup struct {
	GroupId    string
	Members    []string
	ParentKind string
	ParentName string
}
