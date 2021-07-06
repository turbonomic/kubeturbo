package util

const (
	NanoToUnit float64 = 1e9

	BytesToKilobytes float64 = 1024.0

	MilliToUnit float64 = 1e3

	MegaToKilo float64 = 1e3
)

const (
	BASE2UNIT float64 = 1 << (10 * iota)
	BASE2KILO
	BASE2MEGA
	// Add more when needed
)

const (
	METRICKILO float64 = 1e3
	METRICMEGA         = METRICKILO * 1e3
	METRICGIGA         = METRICMEGA * 1e3
	// Add more when needed
)

func MetricNanoToUnit(val float64) float64 {
	return val / METRICGIGA
}

func MetricNanoToMilli(val float64) float64 {
	return val / METRICMEGA
}

func MetricMilliToUnit(val float64) float64 {
	return val / METRICKILO
}

func MetricUnitToMilli(val float64) float64 {
	return val * METRICKILO
}

func MetricKiloToMega(val float64) float64 {
	return val / METRICKILO
}

func Base2BytesToKilobytes(val float64) float64 {
	return val / BASE2KILO
}

func Base2BytesToMegabytes(val float64) float64 {
	return val / BASE2MEGA
}

func Base2MegabytesToBytes(val float64) float64 {
	return val * BASE2MEGA
}
