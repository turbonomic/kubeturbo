package repository

type PolicyGroup struct {
	GroupId    string
	Members    []string
	ParentKind string
	ParentName string
}
