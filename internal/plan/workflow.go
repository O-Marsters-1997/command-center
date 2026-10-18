package plan

type StepName string

type Step struct {
	Name StepName
}

type Workflow struct {
	Name  string
	Hash  string
	Steps []Step
}
