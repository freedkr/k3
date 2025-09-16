package main

import (
	"fmt"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("   Mooncake KV Cache 管理模拟器 v1.0   ")
	fmt.Println("========================================")
	fmt.Println("基于Mooncake论文的分布式LLM推理系统缓存管理模拟")
	fmt.Println()

	// 运行稳健性对比分析
	RunRobustnessComparison()

	// 运行β灵敏度分析
	// RunBetaSensitivityAnalysis()

	// 测试修复后的负载均衡
	// TestLoadBalanceFix()

	// 首先分析trace数据访问模式
	// RunTraceAnalysis()

	// 前缀匹配 vs 简单匹配对比测试
	// RunPrefixMatchComparison()

	// LRU实现对比测试（如果该函数存在的话）
	// CompareWithStandardLRU()

	// Random vs CacheAware 深度对比分析
	// RunRandomVsAwareAnalysis()

	// CacheAware集中化根本原因分析
	// RunCacheAwareAnalysis()

	// 热点迁移机制测试
	// RunHotspotMigrationTest()

	// // 迁移机制深度分析
	// RunMigrationAnalysis()

	// // 通用性策略分析
	// RunUniversalAnalysis()

	// // 运行完整模拟
	// RunSimulation()
}
