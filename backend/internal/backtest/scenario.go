package backtest

import (
	"time"

	"solana-meme-backtest/backend/internal/model"
)

// ScenarioDetector 抽象“先形成结构，再等待触发”的场景识别器，
// 方便后续在同一套窗口和价位框架下继续扩展别的 K 线场景。
type ScenarioDetector interface {
	Code() string
	AnnotateHistorical(levels []model.PriceLevel, window []model.Kline, future []model.Kline, options LevelOptions)
	DetectRealtimeSignals(levels []model.PriceLevel, window []model.Kline, current model.Kline, options LevelOptions) []RealtimeScenarioSignal
}

type RealtimeScenarioSignal struct {
	ScenarioCode        string                 `json:"scenarioCode"`
	ScenarioName        string                 `json:"scenarioName"`
	WindowIndex         int                    `json:"windowIndex"`
	LevelIndex          int                    `json:"levelIndex"`
	LevelType           model.LevelType        `json:"levelType"`
	LevelMarketCap      float64                `json:"levelMarketCap"`
	LevelLowerMarketCap float64                `json:"levelLowerMarketCap"`
	LevelUpperMarketCap float64                `json:"levelUpperMarketCap"`
	SignalTime          time.Time              `json:"signalTime"`
	SignalMarketCap     float64                `json:"signalMarketCap"`
	BreakoutThreshold   float64                `json:"breakoutThreshold"`
	Reason              string                 `json:"reason"`
	Calculation         model.LevelCalculation `json:"calculation"`
	Breakout            *model.BreakoutSetup   `json:"breakout,omitempty"`
}

type RealtimeSignalResult struct {
	Klines     []model.Kline            `json:"klines"`
	Windows    []WindowLevelResult      `json:"windows"`
	Signals    []RealtimeScenarioSignal `json:"signals"`
	WindowSize int                      `json:"windowSize"`
	WindowStep int                      `json:"windowStep"`
}

func pressureBreakoutDetector() ScenarioDetector {
	return pressureBreakoutScenarioDetector{}
}

func PressureBreakoutDetector() ScenarioDetector {
	return pressureBreakoutDetector()
}

type pressureBreakoutScenarioDetector struct{}

func (pressureBreakoutScenarioDetector) Code() string {
	return "pressure_breakout"
}

func (pressureBreakoutScenarioDetector) AnnotateHistorical(levels []model.PriceLevel, window []model.Kline, future []model.Kline, options LevelOptions) {
	annotateBreakoutSetups(levels, window, future, options)
}

func (pressureBreakoutScenarioDetector) DetectRealtimeSignals(levels []model.PriceLevel, window []model.Kline, current model.Kline, options LevelOptions) []RealtimeScenarioSignal {
	if len(window) == 0 {
		return nil
	}
	signals := make([]RealtimeScenarioSignal, 0)
	for levelIndex := range levels {
		signal := detectRealtimeBreakoutSignal(levels[levelIndex], window, current, options)
		if signal == nil {
			continue
		}
		levels[levelIndex].Type = model.LevelTypeResistance
		levels[levelIndex].Breakout = signal.Breakout
		signal.ScenarioCode = "pressure_breakout"
		signal.ScenarioName = "压力带实时突破"
		signal.LevelIndex = levelIndex + 1
		signals = append(signals, *signal)
	}
	return signals
}
