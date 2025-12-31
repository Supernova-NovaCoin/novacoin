// Package dagknight implements confidence score calculation.
package dagknight

import (
	"math"
	"time"
)

// ConfidenceCalculator computes confidence scores for network conditions.
type ConfidenceCalculator struct {
	engine *Engine
}

// NewConfidenceCalculator creates a new confidence calculator.
func NewConfidenceCalculator(engine *Engine) *ConfidenceCalculator {
	return &ConfidenceCalculator{engine: engine}
}

// CalculateConfidence returns a 0-100 confidence score for current conditions.
func (cc *ConfidenceCalculator) CalculateConfidence() uint8 {
	state := cc.engine.latencyMonitor.GetNetworkState()
	if state == nil {
		return 50 // Unknown
	}

	// Factors affecting confidence:
	// 1. Latency (lower = higher confidence)
	// 2. Stability (higher = higher confidence)
	// 3. Active validators (more = higher confidence)

	latencyScore := cc.computeLatencyScore(state.MedianLatency)
	stabilityScore := 100.0 * state.StabilityScore
	validatorScore := cc.computeValidatorScore(state.ActiveValidators)

	// Weighted average
	confidence := latencyScore*0.4 + stabilityScore*0.4 + validatorScore*0.2

	return uint8(math.Min(100, math.Max(0, confidence)))
}

// computeLatencyScore converts latency to a 0-100 score.
func (cc *ConfidenceCalculator) computeLatencyScore(latencyMs float64) float64 {
	config := cc.engine.GetConfig()

	if latencyMs <= config.HighConfidenceLatency {
		return 100.0
	}
	if latencyMs >= config.LowConfidenceLatency {
		return 0.0
	}

	// Linear interpolation between thresholds
	range_ := config.LowConfidenceLatency - config.HighConfidenceLatency
	normalized := (latencyMs - config.HighConfidenceLatency) / range_
	return 100.0 * (1.0 - normalized)
}

// computeValidatorScore converts validator count to a 0-100 score.
func (cc *ConfidenceCalculator) computeValidatorScore(count int) float64 {
	// More validators = higher confidence, capped at 100 validators
	return math.Min(float64(count), 100.0)
}

// CalculateBlockConfidence returns confidence for a specific block based on its context.
func (cc *ConfidenceCalculator) CalculateBlockConfidence(
	observedLatencyMs uint32,
	adaptiveK float32,
	validatorCount int,
) uint8 {
	// Block-level confidence based on conditions at block creation

	latencyScore := cc.computeLatencyScore(float64(observedLatencyMs))

	// K-based confidence: lower K means faster finality, higher confidence
	config := cc.engine.GetConfig()
	kRange := config.MaxK - config.MinK
	kNormalized := (float64(adaptiveK) - config.MinK) / kRange
	kScore := 100.0 * (1.0 - kNormalized) // Lower K = higher score

	validatorScore := cc.computeValidatorScore(validatorCount)

	confidence := latencyScore*0.35 + kScore*0.35 + validatorScore*0.3

	return uint8(math.Min(100, math.Max(0, confidence)))
}

// ConfidenceLevel represents categorical confidence levels.
type ConfidenceLevel int

const (
	// VeryLow indicates poor network conditions.
	VeryLow ConfidenceLevel = iota
	// Low indicates suboptimal conditions.
	Low
	// Medium indicates normal conditions.
	Medium
	// High indicates good conditions.
	High
	// VeryHigh indicates excellent conditions.
	VeryHigh
)

// String returns the string representation of a confidence level.
func (cl ConfidenceLevel) String() string {
	switch cl {
	case VeryLow:
		return "very_low"
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	case VeryHigh:
		return "very_high"
	default:
		return "unknown"
	}
}

// GetConfidenceLevel converts a numeric confidence to a categorical level.
func GetConfidenceLevel(confidence uint8) ConfidenceLevel {
	switch {
	case confidence < 20:
		return VeryLow
	case confidence < 40:
		return Low
	case confidence < 60:
		return Medium
	case confidence < 80:
		return High
	default:
		return VeryHigh
	}
}

// ConfidenceReport contains a detailed confidence analysis.
type ConfidenceReport struct {
	OverallConfidence uint8
	Level             ConfidenceLevel
	LatencyScore      float64
	StabilityScore    float64
	ValidatorScore    float64
	Recommendations   []string
}

// GenerateConfidenceReport creates a detailed confidence report.
func (cc *ConfidenceCalculator) GenerateConfidenceReport() *ConfidenceReport {
	state := cc.engine.latencyMonitor.GetNetworkState()
	if state == nil {
		return &ConfidenceReport{
			OverallConfidence: 50,
			Level:             Medium,
			Recommendations:   []string{"Insufficient network data"},
		}
	}

	latencyScore := cc.computeLatencyScore(state.MedianLatency)
	stabilityScore := 100.0 * state.StabilityScore
	validatorScore := cc.computeValidatorScore(state.ActiveValidators)

	overall := uint8(latencyScore*0.4 + stabilityScore*0.4 + validatorScore*0.2)

	report := &ConfidenceReport{
		OverallConfidence: overall,
		Level:             GetConfidenceLevel(overall),
		LatencyScore:      latencyScore,
		StabilityScore:    stabilityScore,
		ValidatorScore:    validatorScore,
		Recommendations:   make([]string, 0),
	}

	// Generate recommendations
	if latencyScore < 50 {
		report.Recommendations = append(report.Recommendations,
			"High network latency detected. Consider increasing block time.")
	}
	if stabilityScore < 50 {
		report.Recommendations = append(report.Recommendations,
			"Network instability detected. K parameter may fluctuate.")
	}
	if validatorScore < 50 {
		report.Recommendations = append(report.Recommendations,
			"Low validator participation. Consider waiting for more peers.")
	}

	return report
}

// PredictFinalityTime estimates time to finality based on current conditions.
func (cc *ConfidenceCalculator) PredictFinalityTime() (time.Duration, ConfidenceLevel) {
	state := cc.engine.GetAdaptiveState()
	if state == nil || state.NetworkState == nil {
		return 0, VeryLow
	}

	// Mysticeti achieves finality in 3 rounds
	// Finality time = 3 * block_time + network_diameter
	blockTime := state.BlockTime
	networkDelay := time.Duration(state.NetworkState.NetworkDiameter) * time.Millisecond

	finalityTime := 3*blockTime + networkDelay

	confidence := cc.CalculateConfidence()

	return finalityTime, GetConfidenceLevel(confidence)
}
