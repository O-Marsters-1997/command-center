package agentlog

// Offset is the byte the next Advance resumes from.
func (a *Accumulator) Offset() int64 { return a.offset }
