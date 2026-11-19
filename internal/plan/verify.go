package plan

type Caps struct {
	Agents   int
	Commands int
}

type Load struct {
	Agents   int
	Commands int
}

func (l Load) charge(command bool) Load {
	if command {
		l.Commands++
	} else {
		l.Agents++
	}
	return l
}

func (l Load) fits(command bool, caps Caps) bool {
	if command {
		return l.Commands < caps.Commands
	}
	return l.Agents < caps.Agents
}
