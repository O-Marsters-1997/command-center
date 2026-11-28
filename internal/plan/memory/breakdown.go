package memory

type RunPeak struct {
	RSS, Total int64
}

func (p RunPeak) Heavy() bool {
	return p.Total > 0 && float64(p.RSS) >= SpikeRatio*float64(p.Total)
}
