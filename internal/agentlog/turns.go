package agentlog

type requestLedger struct {
	order []string
	phase map[string]int
	usage map[string]RequestUsage
}

func (l *requestLedger) note(parsed logLine, phase int) {
	if parsed.Type != "assistant" || parsed.RequestID == "" {
		return
	}
	if l.phase == nil {
		l.phase = make(map[string]int)
		l.usage = make(map[string]RequestUsage)
	}
	id := parsed.RequestID
	if seen, ok := l.phase[id]; !ok {
		l.order = append(l.order, id)
		l.phase[id] = phase
	} else if seen < 0 {
		l.phase[id] = phase
	}
	if parsed.Message.Usage != (usage{}) {
		l.usage[id] = requestUsage(parsed.Message.Model, parsed.Timestamp, parsed.Message.Usage)
	}
}

func (l *requestLedger) settle(phases []Phase) {
	if len(phases) == 0 {
		return
	}
	for _, id := range l.order {
		phase := &phases[max(0, l.phase[id])]
		phase.Turns++
		phase.Spend += Weight(l.usage[id])
	}
}
