package main

import (
	"fmt"
	"strings"
	"time"
)

func main() {
	fmt.Println("Mooncake KV Cache 分布式缓存策略测试")
	fmt.Println(strings.Repeat("=", 60))

	startTime := time.Now()
	runDirectValidation()
	fmt.Printf("\n测试完成，耗时: %.1f秒\n", time.Since(startTime).Seconds())
}

// runDirectValidation 直接验证核心结论
func runDirectValidation() {
	// 加载数据
	fmt.Println("加载测试数据...")
	requests, err := LoadRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("❌ 数据加载失败: %v\n", err)
		return
	}

	testRequests := requests

	fmt.Printf("使用%d个请求进行验证\n\n", len(testRequests))

	// 测试核心策略
	strategies := []struct {
		name     string
		selector PrefillNodeSelector
	}{
		{"Random-随机选择", &RandomNodeSelector{}},
		{"CacheAware-缓存感知选择器", &CacheAwareSelector{}},
		{"Enhanced-增强策略(β=0.0纯缓存优化)", NewEnhancedCacheAwareSelector(0.6, 0.0)},
		{"Enhanced-增强策略(β=1.2缓存负载均衡)", NewEnhancedCacheAwareSelector(0.6, 1.2)},
		{"PrefixAwareHotspot-前缀感知热点迁移(论文方法)", NewPrefixAwareHotspotSelector(0.6, 0.8, 0.4, 0.1)},
		{"PrefixAwareHotspot-前缀优化版(强化前缀权重)", NewPrefixAwareHotspotSelector(0.5, 0.6, 0.8, 0.15)},
	}

	fmt.Println("\n📊 策略性能测试结果:")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("%-45s %10s %10s\n", "策略名称", "命中率", "负载集中度")
	fmt.Println(strings.Repeat("-", 70))

	results := make([]TestResult, 0)

	for _, strategy := range strategies {
		result := runQuickTest(strategy.selector, testRequests, strategy.name)
		results = append(results, result)

		fmt.Printf("%-45s %9.1f%% %9.1f%%\n",
			strategy.name,
			result.HitRate*100,
			result.Concentration*100)
	}

	fmt.Println(strings.Repeat("-", 65))

	// 显示关键数据对比
	showDataComparison(results)
}

// TestResult 测试结果
type TestResult struct {
	Name          string
	HitRate       float64
	Concentration float64
}

// runQuickTest 快速测试单个策略
func runQuickTest(selector PrefillNodeSelector, requests []*Request, name string) TestResult {
	// 创建模拟器 (4节点, 500缓存容量, LFU淘汰)
	nodeCount := 4
	cacheSize := 500
	sim := NewSimulator(nodeCount, cacheSize, selector, func() EvictionAlgorithm { return NewLFUEviction() })

	// 统计节点负载
	nodeLoads := make(map[string]int)

	// 运行模拟
	for _, request := range requests {
		result, err := sim.processor.ProcessRequest(request, sim.nodes)
		if err != nil {
			continue
		}
		nodeLoads[result.SelectedNode.ID]++
	}

	// 计算指标
	stats := sim.processor.GetStatistics()

	// 计算集中度
	maxLoad := 0
	totalLoad := 0
	for _, count := range nodeLoads {
		if count > maxLoad {
			maxLoad = count
		}
		totalLoad += count
	}

	concentration := float64(maxLoad) / float64(totalLoad)

	return TestResult{
		Name:          name,
		HitRate:       stats.HitRate,
		Concentration: concentration,
	}
}

// showDataComparison 显示关键数据对比
func showDataComparison(results []TestResult) {
	// 找到最佳结果
	var bestHitRate, bestConcentration TestResult
	var worstHitRate, worstConcentration TestResult

	if len(results) > 0 {
		bestHitRate = results[0]
		worstHitRate = results[0]
		bestConcentration = results[0]
		worstConcentration = results[0]
	}

	for _, r := range results {
		if r.HitRate > bestHitRate.HitRate {
			bestHitRate = r
		}
		if r.HitRate < worstHitRate.HitRate {
			worstHitRate = r
		}
		if r.Concentration < bestConcentration.Concentration {
			bestConcentration = r
		}
		if r.Concentration > worstConcentration.Concentration {
			worstConcentration = r
		}
	}

	fmt.Println("\n📈 关键指标分析")
	fmt.Println(strings.Repeat("-", 60))

	fmt.Printf("最佳命中率: %.2f%% (%s)\n",
		bestHitRate.HitRate*100, extractSimpleName(bestHitRate.Name))
	fmt.Printf("最低负载集中度: %.1f%% (%s)\n",
		bestConcentration.Concentration*100, extractSimpleName(bestConcentration.Name))

	fmt.Printf("\n命中率提升: %.2f%% (相比基准Random策略)\n",
		(bestHitRate.HitRate - results[0].HitRate)*100)

	// 成本分析
	fmt.Printf("\n💰 成本效益分析 (基于真实硬件成本):\n")
	fmt.Printf("  GPU计算成本: ~$3/小时 (A100)\n")
	fmt.Printf("  存储成本: ~$0.02/GB/小时\n")
	fmt.Printf("  成本比例: GPU:存储 ≈ 150:1\n")
	fmt.Printf("\n  关键洞察: 1%%命中率提升可节省1%%GPU时间\n")
	fmt.Printf("           而100%%数据冗余仅增加<1%%总成本\n")

	// 计算综合评分（考虑GPU成本）
	fmt.Printf("\n📊 策略综合评分:\n")
	fmt.Printf("  [传统评分: 命中率/集中度]\n")
	for _, r := range results {
		score := r.HitRate / r.Concentration
		quality := "⚠️ 低效"
		if score > 1.0 {
			quality = "✅ 高效"
		} else if score > 0.8 {
			quality = "⭐ 中等"
		}
		fmt.Printf("    %-20s: %.3f %s\n",
			extractSimpleName(r.Name), score, quality)
	}

	fmt.Printf("\n  [GPU成本加权评分: 考虑计算成本远高于存储成本]\n")
	// 更真实的成本权重: GPU成本是存储成本的100倍
	// 每1%命中率提升 = 节省100单位GPU成本
	// 每1%存储冗余 = 增加1单位存储成本
	for _, r := range results {
		// 命中率收益（相对于最低命中率）
		hitRateGain := (r.HitRate - worstHitRate.HitRate) * 100 * 100 // GPU成本权重100x
		// 存储冗余成本（负载集中度越低，冗余越多）
		storageCost := (100 - r.Concentration*100) * 1 // 存储成本权重1x
		// 净收益 = GPU节省 - 存储成本
		netBenefit := hitRateGain - storageCost

		assessment := "❌ 亏损"
		if netBenefit > 500 {
			assessment = "💎 极优"
		} else if netBenefit > 200 {
			assessment = "✅ 优秀"
		} else if netBenefit > 0 {
			assessment = "⭐ 正收益"
		}

		fmt.Printf("    %-20s: %+.1f %s (GPU节省:%.1f - 存储成本:%.1f)\n",
			extractSimpleName(r.Name), netBenefit, assessment, hitRateGain, storageCost)
	}
}

// extractSimpleName 提取策略简称
func extractSimpleName(fullName string) string {
	if strings.Contains(fullName, "Random") {
		return "Random"
	} else if strings.Contains(fullName, "CacheAware") && !strings.Contains(fullName, "Enhanced") && !strings.Contains(fullName, "Prefix") {
		return "CacheAware"
	} else if strings.Contains(fullName, "β=0.0") {
		return "Enhanced(纯缓存)"
	} else if strings.Contains(fullName, "β=1.2") {
		return "Enhanced(均衡)"
	} else if strings.Contains(fullName, "论文方法") {
		return "PrefixAware(论文)"
	} else if strings.Contains(fullName, "强化前缀") {
		return "PrefixAware(优化)"
	}
	return "Unknown"
}
