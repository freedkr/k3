package main

import (
	"bufio"
	"container/list"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
)

// ============= 核心数据结构 =============

// Block 表示一个KV Cache块
type Block struct {
	HashID    int // 块的hash标识
	Size      int // 块大小（token数，默认512）
	HitCount  int // 命中次数
	AccessSeq int // 访问序号（替代LastAccess时间戳）
	CreateSeq int // 创建序号（替代CreateTime时间戳）
	RefCount  int // 引用计数（用于热点检测）
}

// PrefixPattern 前缀模式定义
type PrefixPattern struct {
	Prefix       []int     // 前缀序列
	HitCount     int       // 该前缀的命中次数
	NodeDist     map[string]int // 各节点上该前缀的分布
	LastHit      int       // 最后命中的访问序号
	Intensity    float64   // 热点强度 = HitCount / 时间窗口
	HitHistory   []int     // 命中历史记录 (用于预测分析)
	TrendSlope   float64   // 访问趋势斜率 (正值表示上升趋势)
	PredictedHot bool      // 预测是否会成为热点
}

// HotspotMetrics 热点检测指标
type HotspotMetrics struct {
	PrefixPatterns    map[string]*PrefixPattern // prefix_key -> PrefixPattern
	HotspotThreshold  float64                   // 热点强度阈值
	ReplicationFactor map[string]int            // prefix_key -> 复制因子
	MigrationHistory  []MigrationRecord         // 迁移历史记录
}

// MigrationRecord 迁移记录
type MigrationRecord struct {
	PrefixKey    string    // 迁移的前缀键
	FromNode     string    // 源节点
	ToNode       string    // 目标节点
	Timestamp    int       // 迁移时间戳
	Reason       string    // 迁移原因 (hotspot/balancing)
	Intensity    float64   // 触发时的热点强度
}

// Request 表示一个推理请求
type Request struct {
	Timestamp    int   `json:"timestamp"`     // 到达时间（毫秒）
	InputLength  int   `json:"input_length"`  // 输入token数
	OutputLength int   `json:"output_length"` // 输出token数
	HashIDs      []int `json:"hash_ids"`      // 块的hash ID列表
}

// PrefillNode 表示一个prefill节点
type PrefillNode struct {
	ID               string
	CacheBlocks      map[int]*Block    // 缓存的blocks
	MaxCacheSize     int               // 最大缓存块数
	MaxMemoryMB      int               // 最大内存（MB）
	UsedMemoryMB     float64           // 已使用内存
	TotalHits        int               // 总命中次数
	TotalMisses      int               // 总未命中次数
	EvictionAlgo     EvictionAlgorithm // 淘汰算法
	RequestQueue     []*Request        // 请求队列
	ProcessingTime   float64           // 处理时间（毫秒）
	NetworkBandwidth float64           // 网络带宽（GB/s）

	// 序号计数器（替代时间戳）
	seqCounter int // 全局序号计数器

	// 热点检测和迁移相关
	HotspotMetrics *HotspotMetrics // 热点检测指标
}

// ============= 抽象接口定义 =============

// PrefillNodeSelector prefill节点选择器接口
type PrefillNodeSelector interface {
	// SelectNode 选择一个prefill节点处理请求
	SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode
	// GetName 获取选择器名称
	GetName() string
}

// EvictionAlgorithm 缓存淘汰算法接口
type EvictionAlgorithm interface {
	// Evict 选择要淘汰的block
	Evict(blocks map[int]*Block) int
	// UpdateOnAccess 访问block时更新状态
	UpdateOnAccess(block *Block)
	// OnAdd 添加新block时的回调（可选实现）
	OnAdd(blockID int)
	// GetName 获取算法名称
	GetName() string
}

// PrefillProcessor prefill处理器接口
type PrefillProcessor interface {
	// ProcessRequest 处理一个prefill请求
	ProcessRequest(request *Request, nodes []*PrefillNode) (*PrefillResult, error)
	// GetStatistics 获取统计信息
	GetStatistics() *SimulationStats
}

// ============= 结果和统计结构 =============

// PrefillResult prefill处理结果
type PrefillResult struct {
	SelectedNode    *PrefillNode
	CacheHits       int     // 命中的块数
	CacheMisses     int     // 未命中的块数
	ProcessedBlocks []int   // 处理的块ID列表
	TransferTime    float64 // 传输时间（毫秒）
	ProcessTime     float64 // 处理时间（毫秒）
}

// SimulationStats 模拟统计信息
type SimulationStats struct {
	TotalRequests   int
	TotalHits       int
	TotalMisses     int
	HitRate         float64
	AvgTransferTime float64
	AvgProcessTime  float64
	NodeStats       map[string]*NodeStatistics
}

// NodeStatistics 节点统计信息
type NodeStatistics struct {
	NodeID         string
	TotalRequests  int
	TotalHits      int
	TotalMisses    int
	HitRate        float64
	AvgMemoryUsage float64
	MaxMemoryUsage float64
	EvictedBlocks  int
}

// ============= 接口实现：随机选择器 =============

type RandomNodeSelector struct{}

func (r *RandomNodeSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}
	return nodes[rand.Intn(len(nodes))]
}

func (r *RandomNodeSelector) GetName() string {
	return "Random"
}

// ============= 接口实现：缓存感知选择器 =============

type CacheAwareSelector struct{}

func (c *CacheAwareSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	// 计算每个节点的缓存命中预期
	type nodeScore struct {
		node     *PrefillNode
		hitCount int
		load     float64
	}

	scores := make([]nodeScore, len(nodes))

	for i, node := range nodes {
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}

		// 考虑负载因素 (修复: 使用更合理的负载计算)
		// 负载 = 队列长度 / 100 (标准化到0-1范围)
		load := float64(len(node.RequestQueue)) / 100.0
		scores[i] = nodeScore{
			node:     node,
			hitCount: hitCount,
			load:     load,
		}
	}

	// 选择得分最高的节点（命中多、负载低）
	// 修复: 确保负载权重有效，乘以系数增强负载影响
	bestNode := scores[0].node
	bestScore := float64(scores[0].hitCount) - scores[0].load*10.0

	for _, score := range scores[1:] {
		currentScore := float64(score.hitCount) - score.load*10.0
		if currentScore > bestScore {
			bestScore = currentScore
			bestNode = score.node
		}
	}

	return bestNode
}

// EnhancedCacheAwareSelector 增强版缓存感知选择器（包含α、β权重）
type EnhancedCacheAwareSelector struct {
	Alpha float64 // 缓存亲和性权重 (论文中的α)
	Beta  float64 // 负载均衡权重 (论文中的β)
}

func NewEnhancedCacheAwareSelector(alpha, beta float64) *EnhancedCacheAwareSelector {
	return &EnhancedCacheAwareSelector{
		Alpha: alpha,
		Beta:  beta,
	}
}

func (e *EnhancedCacheAwareSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	bestNode := nodes[0]
	bestScore := e.calculateScore(request, nodes[0], nodes)

	for _, node := range nodes[1:] {
		score := e.calculateScore(request, node, nodes)
		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode
}

func (e *EnhancedCacheAwareSelector) calculateScore(request *Request, node *PrefillNode, allNodes []*PrefillNode) float64 {
	// 1. 计算缓存命中率 (归一化到[0,1])
	hitCount := 0
	for _, hashID := range request.HashIDs {
		if _, exists := node.CacheBlocks[hashID]; exists {
			hitCount++
		}
	}
	hitRatio := float64(hitCount) / float64(len(request.HashIDs))

	// 2. 计算归一化负载 (修复: 使用合理的基数)
	// 使用100作为标准化基数，而不是MaxCacheSize
	currentLoad := float64(len(node.RequestQueue)) / 100.0

	// 归一化：相对于所有节点的平均负载
	totalLoad := 0.0
	for _, n := range allNodes {
		totalLoad += float64(len(n.RequestQueue)) / 100.0
	}
	avgLoad := totalLoad / float64(len(allNodes))
	normalizedLoad := currentLoad
	if avgLoad > 0 {
		normalizedLoad = currentLoad / avgLoad
	}

	// 3. 应用α、β权重计算最终得分
	score := e.Alpha*hitRatio - e.Beta*normalizedLoad

	return score
}

func (e *EnhancedCacheAwareSelector) GetName() string {
	return fmt.Sprintf("EnhancedCacheAware(α=%.1f,β=%.1f)", e.Alpha, e.Beta)
}

func (c *CacheAwareSelector) GetName() string {
	return "CacheAware"
}

// ============= 接口实现：前缀感知热点迁移选择器 =============

type PrefixAwareHotspotSelector struct {
	Alpha             float64 // 缓存亲和性权重
	Beta              float64 // 负载均衡权重
	Gamma             float64 // 前缀匹配权重
	HotspotThreshold  float64 // 热点强度阈值
	TimeWindowSize    int     // 热点检测时间窗口
	MaxPrefixLength   int     // 最大前缀长度
	accessCounter     int     // 全局访问计数器
}

func NewPrefixAwareHotspotSelector(alpha, beta, gamma, hotspotThreshold float64) *PrefixAwareHotspotSelector {
	return &PrefixAwareHotspotSelector{
		Alpha:             alpha,
		Beta:              beta,
		Gamma:             gamma,
		HotspotThreshold:  hotspotThreshold,
		TimeWindowSize:    1000, // 1000个请求的时间窗口
		MaxPrefixLength:   8,    // 最大前缀长度为8
		accessCounter:     0,
	}
}

func (p *PrefixAwareHotspotSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	p.accessCounter++

	// 初始化节点的热点检测系统（如果需要）
	for _, node := range nodes {
		if node.HotspotMetrics == nil {
			node.HotspotMetrics = &HotspotMetrics{
				PrefixPatterns:    make(map[string]*PrefixPattern),
				HotspotThreshold:  p.HotspotThreshold,
				ReplicationFactor: make(map[string]int),
				MigrationHistory:  make([]MigrationRecord, 0),
			}
		}
	}

	// 1. 执行热点迁移检测和处理
	p.detectAndMigrateHotspots(request, nodes)

	// 2. 进行增强的节点选择
	bestNode := p.selectBestNodeWithPrefixAwareness(request, nodes)

	// 3. 更新前缀模式统计
	p.updatePrefixPatterns(request, bestNode)

	return bestNode
}

// detectAndMigrateHotspots 检测并处理热点迁移
func (p *PrefixAwareHotspotSelector) detectAndMigrateHotspots(request *Request, nodes []*PrefillNode) {
	// 检测当前请求的前缀是否构成热点
	for prefixLen := min(p.MaxPrefixLength, len(request.HashIDs)); prefixLen >= 2; prefixLen-- {
		prefix := request.HashIDs[:prefixLen]
		prefixKey := p.hashIDsToKey(prefix)

		// 查找拥有此前缀最多blocks的节点
		maxHitNode, maxHits := p.findBestPrefixNode(prefix, nodes)
		if maxHitNode == nil || maxHits == 0 {
			continue
		}

		pattern := maxHitNode.HotspotMetrics.PrefixPatterns[prefixKey]
		if pattern != nil {
			// 1. 执行预测性分析
			p.updatePredictiveAnalysis(pattern)

			// 2. 检查是否构成当前热点或预测热点
			isCurrentHotspot := p.isHotspot(pattern)
			isPredictedHotspot := pattern.PredictedHot && pattern.TrendSlope > 0.1

			if isCurrentHotspot || isPredictedHotspot {
				// 根据热点类型选择迁移策略
				migrationReason := "current_hotspot"
				if isPredictedHotspot && !isCurrentHotspot {
					migrationReason = "predicted_hotspot"
				}

				// 执行预测性热点迁移
				p.executeHotspotMigrationWithPrediction(prefixKey, pattern, maxHitNode, nodes, migrationReason)
			}
		}
	}
}

// updatePredictiveAnalysis 更新预测性分析
func (p *PrefixAwareHotspotSelector) updatePredictiveAnalysis(pattern *PrefixPattern) {
	// 1. 维护固定长度的命中历史窗口
	historyWindowSize := 20 // 保留最近20个数据点

	// 添加当前命中计数到历史
	if len(pattern.HitHistory) >= historyWindowSize {
		// 移除最旧的记录
		pattern.HitHistory = pattern.HitHistory[1:]
	}
	pattern.HitHistory = append(pattern.HitHistory, pattern.HitCount)

	// 2. 计算访问趋势斜率（简单线性回归）
	if len(pattern.HitHistory) >= 5 { // 至少需要5个数据点才能计算趋势
		pattern.TrendSlope = p.calculateTrendSlope(pattern.HitHistory)
	}

	// 3. 基于多个指标进行热点预测
	pattern.PredictedHot = p.predictFutureHotspot(pattern)
}

// calculateTrendSlope 计算访问趋势斜率
func (p *PrefixAwareHotspotSelector) calculateTrendSlope(hitHistory []int) float64 {
	n := len(hitHistory)
	if n < 2 {
		return 0.0
	}

	// 简单线性回归 y = ax + b，计算斜率a
	var sumX, sumY, sumXY, sumX2 float64

	for i, hits := range hitHistory {
		x := float64(i)
		y := float64(hits)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// 斜率 a = (n*∑xy - ∑x*∑y) / (n*∑x² - (∑x)²)
	denominator := float64(n)*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0.0
	}

	slope := (float64(n)*sumXY - sumX*sumY) / denominator
	return slope
}

// predictFutureHotspot 预测未来热点
func (p *PrefixAwareHotspotSelector) predictFutureHotspot(pattern *PrefixPattern) bool {
	// 综合多个指标进行预测

	// 1. 趋势指标：斜率为正且增长快速
	trendScore := 0.0
	if pattern.TrendSlope > 0.05 { // 明显上升趋势
		trendScore = 1.0
	} else if pattern.TrendSlope > 0.01 { // 缓慢上升趋势
		trendScore = 0.5
	}

	// 2. 强度指标：接近热点阈值
	intensityScore := 0.0
	intensityRatio := pattern.Intensity / p.HotspotThreshold
	if intensityRatio > 0.7 { // 接近阈值
		intensityScore = 1.0
	} else if intensityRatio > 0.5 {
		intensityScore = 0.7
	} else if intensityRatio > 0.3 {
		intensityScore = 0.3
	}

	// 3. 频率指标：最近访问频率
	frequencyScore := 0.0
	if len(pattern.HitHistory) >= 3 {
		recentHits := pattern.HitHistory[len(pattern.HitHistory)-3:] // 最近3次
		avgRecentHits := 0
		for _, hits := range recentHits {
			avgRecentHits += hits
		}
		avgRecentHits /= len(recentHits)

		if avgRecentHits > pattern.HitCount/2 { // 近期活跃度高
			frequencyScore = 1.0
		} else if avgRecentHits > pattern.HitCount/4 {
			frequencyScore = 0.6
		}
	}

	// 4. 综合预测分数
	predictionScore := (trendScore*0.4 + intensityScore*0.4 + frequencyScore*0.2)

	// 预测阈值：分数 > 0.6 认为有热点风险
	return predictionScore > 0.6
}

// executeHotspotMigrationWithPrediction 执行带预测的热点迁移
func (p *PrefixAwareHotspotSelector) executeHotspotMigrationWithPrediction(prefixKey string, pattern *PrefixPattern, sourceNode *PrefillNode, allNodes []*PrefillNode, reason string) {
	// 1. 根据预测结果调整迁移策略
	var replicationFactor int
	if reason == "predicted_hotspot" {
		// 预测性迁移：更保守的复制策略
		replicationFactor = p.calculatePredictiveReplicationFactor(pattern, allNodes)
	} else {
		// 当前热点迁移：标准动态复制策略
		replicationFactor = p.calculateDynamicReplicationFactor(pattern, allNodes)
	}

	// 2. 更新复制因子记录
	sourceNode.HotspotMetrics.ReplicationFactor[prefixKey] = replicationFactor

	// 3. 选择目标节点（预测性迁移优先选择负载最低的节点）
	targetNodes := p.selectOptimalTargetNodes(sourceNode, allNodes, replicationFactor)

	// 4. 执行分布式复制迁移
	migratedCount := 0
	for _, targetNode := range targetNodes {
		// 执行前缀相关blocks的迁移到每个目标节点
		for _, hashID := range pattern.Prefix {
			if block, exists := sourceNode.CacheBlocks[hashID]; exists {
				// 检查目标节点是否有足够空间
				if len(targetNode.CacheBlocks) < targetNode.MaxCacheSize {
					// 复制block（而不是移动）
					targetNode.CacheBlocks[hashID] = &Block{
						HashID:    block.HashID,
						Size:      block.Size,
						HitCount:  1, // 重置命中次数
						AccessSeq: targetNode.seqCounter + 1,
						CreateSeq: targetNode.seqCounter + 1,
					}
					targetNode.seqCounter++
					migratedCount++

					// 通知目标节点的淘汰算法
					if targetNode.EvictionAlgo != nil {
						targetNode.EvictionAlgo.OnAdd(hashID)
					}
				}
			}
		}

		// 记录每次迁移
		if migratedCount > 0 {
			sourceNode.HotspotMetrics.MigrationHistory = append(sourceNode.HotspotMetrics.MigrationHistory, MigrationRecord{
				PrefixKey: prefixKey,
				FromNode:  sourceNode.ID,
				ToNode:    targetNode.ID,
				Timestamp: p.accessCounter,
				Reason:    fmt.Sprintf("%s_rf_%d_trend_%.3f", reason, replicationFactor, pattern.TrendSlope),
				Intensity: pattern.Intensity,
			})
		}
	}
}

// calculatePredictiveReplicationFactor 计算预测性复制因子
func (p *PrefixAwareHotspotSelector) calculatePredictiveReplicationFactor(pattern *PrefixPattern, allNodes []*PrefillNode) int {
	// 基础复制因子
	baseReplicas := 1

	// 根据预测置信度和趋势强度确定额外副本数
	var additionalReplicas int

	// 考虑趋势斜率：斜率越大，需要的副本越多
	if pattern.TrendSlope >= 0.2 && pattern.Intensity >= 0.05 {
		// 强上升趋势，预防性多副本
		additionalReplicas = min(len(allNodes)-1, 2)
	} else if pattern.TrendSlope >= 0.1 && pattern.Intensity >= 0.03 {
		// 中等上升趋势，预防性单副本
		additionalReplicas = min(len(allNodes)-1, 1)
	} else if pattern.TrendSlope >= 0.05 {
		// 轻微上升趋势，保守预防
		additionalReplicas = min(len(allNodes)-1, 1)
	} else {
		// 趋势不明显，无需额外复制
		additionalReplicas = 0
	}

	return baseReplicas + additionalReplicas
}

// selectBestNodeWithPrefixAwareness 基于前缀感知的增强节点选择
func (p *PrefixAwareHotspotSelector) selectBestNodeWithPrefixAwareness(request *Request, nodes []*PrefillNode) *PrefillNode {
	type nodeScore struct {
		node         *PrefillNode
		cacheScore   float64 // 基础缓存命中得分
		prefixScore  float64 // 前缀匹配得分
		loadScore    float64 // 负载得分
		finalScore   float64 // 最终综合得分
	}

	scores := make([]nodeScore, len(nodes))

	for i, node := range nodes {
		// 1. 计算基础缓存命中得分
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}
		cacheScore := float64(hitCount) / float64(len(request.HashIDs))

		// 2. 计算前缀匹配得分（考虑多个前缀长度）
		prefixScore := p.calculatePrefixScore(request, node)

		// 3. 计算负载得分
		currentLoad := float64(len(node.RequestQueue)) / 100.0
		loadScore := 1.0 / (1.0 + currentLoad) // 负载越低得分越高

		// 4. 综合得分计算
		finalScore := p.Alpha*cacheScore + p.Gamma*prefixScore + p.Beta*loadScore

		scores[i] = nodeScore{
			node:        node,
			cacheScore:  cacheScore,
			prefixScore: prefixScore,
			loadScore:   loadScore,
			finalScore:  finalScore,
		}
	}

	// 选择得分最高的节点
	bestScore := scores[0]
	for _, score := range scores[1:] {
		if score.finalScore > bestScore.finalScore {
			bestScore = score
		}
	}

	return bestScore.node
}

// calculatePrefixScore 计算前缀匹配得分
func (p *PrefixAwareHotspotSelector) calculatePrefixScore(request *Request, node *PrefillNode) float64 {
	maxScore := 0.0

	// 检查不同长度的前缀
	for prefixLen := min(p.MaxPrefixLength, len(request.HashIDs)); prefixLen >= 2; prefixLen-- {
		prefix := request.HashIDs[:prefixLen]

		// 计算连续前缀命中长度
		continuousLen := 0
		for i, hashID := range prefix {
			if _, exists := node.CacheBlocks[hashID]; exists {
				continuousLen = i + 1
			} else {
				break
			}
		}

		// 前缀得分 = (连续长度 / 前缀总长度) * 前缀长度权重
		prefixScore := (float64(continuousLen) / float64(prefixLen)) * float64(prefixLen)

		if prefixScore > maxScore {
			maxScore = prefixScore
		}
	}

	// 归一化到 [0, 1]
	return maxScore / float64(p.MaxPrefixLength)
}

// updatePrefixPatterns 更新前缀模式统计
func (p *PrefixAwareHotspotSelector) updatePrefixPatterns(request *Request, selectedNode *PrefillNode) {
	// 更新各种长度的前缀模式
	for prefixLen := min(p.MaxPrefixLength, len(request.HashIDs)); prefixLen >= 2; prefixLen-- {
		prefix := request.HashIDs[:prefixLen]
		prefixKey := p.hashIDsToKey(prefix)

		// 获取或创建前缀模式
		pattern, exists := selectedNode.HotspotMetrics.PrefixPatterns[prefixKey]
		if !exists {
			pattern = &PrefixPattern{
				Prefix:       prefix,
				NodeDist:     make(map[string]int),
				HitHistory:   make([]int, 0),
				TrendSlope:   0.0,
				PredictedHot: false,
			}
			selectedNode.HotspotMetrics.PrefixPatterns[prefixKey] = pattern
		}

		// 更新统计
		pattern.HitCount++
		pattern.LastHit = p.accessCounter
		pattern.NodeDist[selectedNode.ID]++

		// 更新热点强度（使用滑动窗口）
		windowStart := p.accessCounter - p.TimeWindowSize
		if windowStart < 0 {
			windowStart = 0
		}
		if pattern.LastHit >= windowStart {
			pattern.Intensity = float64(pattern.HitCount) / float64(p.accessCounter - windowStart + 1)
		}
	}
}

// 工具函数
func (p *PrefixAwareHotspotSelector) hashIDsToKey(hashIDs []int) string {
	key := ""
	for i, id := range hashIDs {
		if i > 0 {
			key += ","
		}
		key += fmt.Sprintf("%d", id)
	}
	return key
}

func (p *PrefixAwareHotspotSelector) findBestPrefixNode(prefix []int, nodes []*PrefillNode) (*PrefillNode, int) {
	bestNode := (*PrefillNode)(nil)
	maxHits := 0

	for _, node := range nodes {
		hits := 0
		for _, hashID := range prefix {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hits++
			}
		}
		if hits > maxHits {
			maxHits = hits
			bestNode = node
		}
	}

	return bestNode, maxHits
}

func (p *PrefixAwareHotspotSelector) isHotspot(pattern *PrefixPattern) bool {
	return pattern.Intensity > p.HotspotThreshold
}


// calculateDynamicReplicationFactor 基于热点强度动态计算复制因子
func (p *PrefixAwareHotspotSelector) calculateDynamicReplicationFactor(pattern *PrefixPattern, allNodes []*PrefillNode) int {
	// 基础复制因子 = 1（原节点）+ 根据热点强度增加的副本数
	baseReplicas := 1

	// 根据热点强度分级确定额外副本数
	var additionalReplicas int
	if pattern.Intensity >= 0.5 {
		// 超高热度：需要多节点分散
		additionalReplicas = min(len(allNodes)-1, 3) // 最多复制到3个额外节点
	} else if pattern.Intensity >= 0.2 {
		// 高热度：需要双节点备份
		additionalReplicas = min(len(allNodes)-1, 2) // 最多复制到2个额外节点
	} else if pattern.Intensity >= 0.1 {
		// 中等热度：需要单节点备份
		additionalReplicas = min(len(allNodes)-1, 1) // 最多复制到1个额外节点
	} else {
		// 低热度：无需额外复制
		additionalReplicas = 0
	}

	return baseReplicas + additionalReplicas
}

// selectOptimalTargetNodes 选择最佳目标节点群
func (p *PrefixAwareHotspotSelector) selectOptimalTargetNodes(sourceNode *PrefillNode, allNodes []*PrefillNode, replicationFactor int) []*PrefillNode {
	// 创建候选节点列表（排除源节点）
	candidates := make([]*PrefillNode, 0)
	for _, node := range allNodes {
		if node.ID != sourceNode.ID {
			candidates = append(candidates, node)
		}
	}

	// 如果需要的副本数超过候选节点数，则使用所有候选节点
	targetCount := min(replicationFactor-1, len(candidates)) // -1是因为不包括源节点
	if targetCount <= 0 {
		return []*PrefillNode{}
	}

	// 按照负载升序排序候选节点
	type nodeWithLoad struct {
		node *PrefillNode
		load float64
	}

	nodeLoads := make([]nodeWithLoad, len(candidates))
	for i, node := range candidates {
		load := float64(len(node.RequestQueue)) + float64(len(node.CacheBlocks))/float64(node.MaxCacheSize)
		nodeLoads[i] = nodeWithLoad{node: node, load: load}
	}

	// 简单冒泡排序按负载排序
	for i := 0; i < len(nodeLoads)-1; i++ {
		for j := 0; j < len(nodeLoads)-i-1; j++ {
			if nodeLoads[j].load > nodeLoads[j+1].load {
				nodeLoads[j], nodeLoads[j+1] = nodeLoads[j+1], nodeLoads[j]
			}
		}
	}

	// 选择负载最低的前N个节点
	selectedNodes := make([]*PrefillNode, targetCount)
	for i := 0; i < targetCount; i++ {
		selectedNodes[i] = nodeLoads[i].node
	}

	return selectedNodes
}

func (p *PrefixAwareHotspotSelector) GetName() string {
	return fmt.Sprintf("PrefixAwareHotspot(α=%.1f,β=%.1f,γ=%.1f,θ=%.1f)", p.Alpha, p.Beta, p.Gamma, p.HotspotThreshold)
}

// 辅助函数：求最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============= 接口实现：FIFO淘汰算法 =============

type FIFOEviction struct {
	insertOrder *list.List            // 维护插入顺序的队列
	orderNodes  map[int]*list.Element // blockID -> 队列中的节点
}

func NewFIFOEviction() *FIFOEviction {
	return &FIFOEviction{
		insertOrder: list.New(),
		orderNodes:  make(map[int]*list.Element),
	}
}

func (f *FIFOEviction) Evict(blocks map[int]*Block) int {
	if f.insertOrder.Len() == 0 {
		return -1
	}

	// 直接返回队列头部（最早插入的）
	front := f.insertOrder.Front()
	if front != nil {
		blockID := front.Value.(int)
		// 从队列和映射中移除
		f.insertOrder.Remove(front)
		delete(f.orderNodes, blockID)
		return blockID
	}
	return -1
}

func (f *FIFOEviction) UpdateOnAccess(block *Block) {
	// FIFO不关心访问时间，只关心插入顺序
	block.HitCount++
}

func (f *FIFOEviction) OnAdd(blockID int) {
	// 添加到队列尾部
	element := f.insertOrder.PushBack(blockID)
	f.orderNodes[blockID] = element
}

func (f *FIFOEviction) GetName() string {
	return "FIFO"
}

// ============= 接口实现：LRU淘汰算法 =============

type LRUEviction struct {
	accessOrder *list.List            // 维护访问顺序的双向链表（头部=最近，尾部=最久）
	orderNodes  map[int]*list.Element // blockID -> 链表中的节点
}

func NewLRUEviction() *LRUEviction {
	return &LRUEviction{
		accessOrder: list.New(),
		orderNodes:  make(map[int]*list.Element),
	}
}

func (l *LRUEviction) Evict(blocks map[int]*Block) int {
	if l.accessOrder.Len() == 0 {
		return -1
	}

	// 直接返回链表尾部（最久未使用的）
	back := l.accessOrder.Back()
	if back != nil {
		blockID := back.Value.(int)
		// 从链表和映射中移除
		l.accessOrder.Remove(back)
		delete(l.orderNodes, blockID)
		return blockID
	}
	return -1
}

func (l *LRUEviction) UpdateOnAccess(block *Block) {
	block.HitCount++
	blockID := block.HashID

	// 将访问的block移动到链表头部（最近使用）
	if element, exists := l.orderNodes[blockID]; exists {
		l.accessOrder.MoveToFront(element)
	}
}

func (l *LRUEviction) OnAdd(blockID int) {
	// 添加到链表头部（最近使用）
	element := l.accessOrder.PushFront(blockID)
	l.orderNodes[blockID] = element
}

func (l *LRUEviction) GetName() string {
	return "LRU"
}

// ============= 接口实现：LFU淘汰算法 =============

type LFUEviction struct {
	freqGroups map[int]*list.List    // 频率 -> 该频率的blocks列表
	blockFreq  map[int]int           // blockID -> 其当前频率
	blockNodes map[int]*list.Element // blockID -> 在频率链表中的位置
	minFreq    int                   // 当前最小频率
}

func NewLFUEviction() *LFUEviction {
	return &LFUEviction{
		freqGroups: make(map[int]*list.List),
		blockFreq:  make(map[int]int),
		blockNodes: make(map[int]*list.Element),
		minFreq:    1,
	}
}

func (l *LFUEviction) Evict(blocks map[int]*Block) int {
	if len(l.blockFreq) == 0 {
		return -1
	}

	// 找到最小频率组
	minFreqList := l.freqGroups[l.minFreq]
	if minFreqList == nil || minFreqList.Len() == 0 {
		// 重新计算最小频率
		l.updateMinFreq()
		minFreqList = l.freqGroups[l.minFreq]
	}

	if minFreqList != nil && minFreqList.Len() > 0 {
		// 选择该频率组中最早加入的（FIFO within same frequency）
		front := minFreqList.Front()
		if front != nil {
			blockID := front.Value.(int)
			l.removeBlock(blockID)
			return blockID
		}
	}

	return -1
}

func (l *LFUEviction) UpdateOnAccess(block *Block) {
	blockID := block.HashID
	block.HitCount++

	// 获取当前频率
	oldFreq, exists := l.blockFreq[blockID]
	if !exists {
		// 新block，初始频率为1
		oldFreq = 0
	}

	newFreq := oldFreq + 1

	// 从旧频率组移除
	if oldFreq > 0 {
		l.removeFromFreqGroup(blockID, oldFreq)
	}

	// 添加到新频率组
	l.addToFreqGroup(blockID, newFreq)
	l.blockFreq[blockID] = newFreq

	// 更新最小频率
	if oldFreq == l.minFreq && (l.freqGroups[oldFreq] == nil || l.freqGroups[oldFreq].Len() == 0) {
		l.minFreq = newFreq
	}
}

func (l *LFUEviction) OnAdd(blockID int) {
	// 新block初始频率为1
	l.addToFreqGroup(blockID, 1)
	l.blockFreq[blockID] = 1
	l.minFreq = 1
}

// 辅助方法
func (l *LFUEviction) removeBlock(blockID int) {
	freq := l.blockFreq[blockID]
	l.removeFromFreqGroup(blockID, freq)
	delete(l.blockFreq, blockID)
}

func (l *LFUEviction) addToFreqGroup(blockID int, freq int) {
	if l.freqGroups[freq] == nil {
		l.freqGroups[freq] = list.New()
	}
	element := l.freqGroups[freq].PushBack(blockID)
	l.blockNodes[blockID] = element
}

func (l *LFUEviction) removeFromFreqGroup(blockID int, freq int) {
	if element, exists := l.blockNodes[blockID]; exists {
		l.freqGroups[freq].Remove(element)
		delete(l.blockNodes, blockID)
	}
}

func (l *LFUEviction) updateMinFreq() {
	l.minFreq++
	for l.freqGroups[l.minFreq] == nil || l.freqGroups[l.minFreq].Len() == 0 {
		l.minFreq++
		if l.minFreq > 1000 { // 防止无限循环
			l.minFreq = 1
			break
		}
	}
}

func (l *LFUEviction) GetName() string {
	return "LFU"
}

// ============= 基础Prefill处理器实现 =============

type BasicPrefillProcessor struct {
	selector     PrefillNodeSelector
	stats        *SimulationStats
	nodeStatsMap map[string]*NodeStatistics
}

func NewBasicPrefillProcessor(selector PrefillNodeSelector) *BasicPrefillProcessor {
	return &BasicPrefillProcessor{
		selector: selector,
		stats: &SimulationStats{
			NodeStats: make(map[string]*NodeStatistics),
		},
		nodeStatsMap: make(map[string]*NodeStatistics),
	}
}

func (p *BasicPrefillProcessor) ProcessRequest(request *Request, nodes []*PrefillNode) (*PrefillResult, error) {
	// 1. 选择节点
	selectedNode := p.selector.SelectNode(request, nodes)
	if selectedNode == nil {
		return nil, fmt.Errorf("no available node")
	}

	// 添加请求到队列 (修复: RequestQueue之前从未更新)
	selectedNode.RequestQueue = append(selectedNode.RequestQueue, request)

	// 保持队列长度合理，模拟请求完成后的清理
	// 只保留最近的100个请求用于负载计算
	if len(selectedNode.RequestQueue) > 100 {
		selectedNode.RequestQueue = selectedNode.RequestQueue[len(selectedNode.RequestQueue)-100:]
	}

	// 初始化节点统计
	if _, exists := p.nodeStatsMap[selectedNode.ID]; !exists {
		p.nodeStatsMap[selectedNode.ID] = &NodeStatistics{
			NodeID: selectedNode.ID,
		}
	}
	nodeStats := p.nodeStatsMap[selectedNode.ID]

	result := &PrefillResult{
		SelectedNode:    selectedNode,
		ProcessedBlocks: request.HashIDs,
	}

	// 2. 处理每个block
	blockSize := 512.0                                 // 每个block的token数
	blockMemoryMB := blockSize * 2 * 4 / (1024 * 1024) // 假设每个token占用2*4字节（KV各4字节）

	for _, hashID := range request.HashIDs {
		if block, exists := selectedNode.CacheBlocks[hashID]; exists {
			// Cache命中
			result.CacheHits++
			selectedNode.TotalHits++
			selectedNode.EvictionAlgo.UpdateOnAccess(block)
		} else {
			// Cache未命中，需要添加
			result.CacheMisses++
			selectedNode.TotalMisses++

			// 检查内存容量
			requiredMemory := blockMemoryMB
			availableMemory := float64(selectedNode.MaxMemoryMB) - selectedNode.UsedMemoryMB

			// 如果内存不足，执行淘汰
			for availableMemory < requiredMemory && len(selectedNode.CacheBlocks) > 0 {
				evictID := selectedNode.EvictionAlgo.Evict(selectedNode.CacheBlocks)
				if evictID == -1 {
					break
				}
				delete(selectedNode.CacheBlocks, evictID)
				selectedNode.UsedMemoryMB -= blockMemoryMB
				nodeStats.EvictedBlocks++
				availableMemory = float64(selectedNode.MaxMemoryMB) - selectedNode.UsedMemoryMB
			}

			// 添加新block
			selectedNode.seqCounter++ // 递增序号计数器
			selectedNode.CacheBlocks[hashID] = &Block{
				HashID:    hashID,
				Size:      512,
				HitCount:  1,
				AccessSeq: selectedNode.seqCounter,
				CreateSeq: selectedNode.seqCounter,
			}
			selectedNode.UsedMemoryMB += blockMemoryMB
			selectedNode.EvictionAlgo.OnAdd(hashID) // 通知淘汰算法
		}
	}

	// 更新统计
	p.stats.TotalRequests++
	p.stats.TotalHits += result.CacheHits
	p.stats.TotalMisses += result.CacheMisses

	nodeStats.TotalRequests++
	nodeStats.TotalHits += result.CacheHits
	nodeStats.TotalMisses += result.CacheMisses

	// 计算处理时间
	result.ProcessTime = float64(request.InputLength) * 0.01 // 简化计算
	result.TransferTime = float64(result.CacheMisses) * blockMemoryMB / selectedNode.NetworkBandwidth

	return result, nil
}

func (p *BasicPrefillProcessor) GetStatistics() *SimulationStats {
	if p.stats.TotalRequests > 0 {
		p.stats.HitRate = float64(p.stats.TotalHits) / float64(p.stats.TotalHits+p.stats.TotalMisses)
	}

	// 计算每个节点的统计
	for nodeID, nodeStats := range p.nodeStatsMap {
		if nodeStats.TotalHits+nodeStats.TotalMisses > 0 {
			nodeStats.HitRate = float64(nodeStats.TotalHits) / float64(nodeStats.TotalHits+nodeStats.TotalMisses)
		}
		p.stats.NodeStats[nodeID] = nodeStats
	}

	return p.stats
}

// ============= 数据加载函数 =============

func LoadRequests(filename string) ([]*Request, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var requests []*Request
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var rawData map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &rawData); err != nil {
			continue
		}

		// 解析数据
		request := &Request{
			Timestamp:    int(rawData["timestamp"].(float64)),
			InputLength:  int(rawData["input_length"].(float64)),
			OutputLength: int(rawData["output_length"].(float64)),
		}

		// 解析hash_ids
		hashIDsRaw := rawData["hash_ids"].([]interface{})
		request.HashIDs = make([]int, len(hashIDsRaw))
		for i, id := range hashIDsRaw {
			request.HashIDs[i] = int(id.(float64))
		}

		requests = append(requests, request)
	}

	return requests, scanner.Err()
}

// ============= 模拟器主类 =============

type Simulator struct {
	nodes        []*PrefillNode
	processor    PrefillProcessor
	requests     []*Request
	selectorName string
}

func NewSimulator(nodeCount int, cacheSize int, selector PrefillNodeSelector, evictionAlgo func() EvictionAlgorithm) *Simulator {
	nodes := make([]*PrefillNode, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = &PrefillNode{
			ID:               fmt.Sprintf("node-%d", i),
			CacheBlocks:      make(map[int]*Block),
			MaxCacheSize:     cacheSize,
			MaxMemoryMB:      2,    // 减小到2MB以确保淘汰
			NetworkBandwidth: 10.0, // 10GB/s
			EvictionAlgo:     evictionAlgo(),
			seqCounter:       0, // 初始化序号计数器
			HotspotMetrics:   nil, // 由PrefixAwareHotspotSelector按需初始化
		}
	}

	return &Simulator{
		nodes:        nodes,
		processor:    NewBasicPrefillProcessor(selector),
		selectorName: selector.GetName(),
	}
}

func (s *Simulator) LoadData(filename string) error {
	requests, err := LoadRequests(filename)
	if err != nil {
		return err
	}
	s.requests = requests
	return nil
}


