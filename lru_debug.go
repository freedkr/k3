package main

import (
	"container/list"
	"fmt"
)

// DebugLRUEviction 调试版本的LRU实现
// 添加详细的操作日志来诊断可能的问题
type DebugLRUEviction struct {
	accessOrder   *list.List            // 维护访问顺序的双向链表 (最近使用的在前)
	orderNodes    map[int]*list.Element // blockID -> 链表中的节点
	operationLog  []string              // 操作日志
	debugEnabled  bool                  // 是否启用调试输出
}

func NewDebugLRUEviction() *DebugLRUEviction {
	return &DebugLRUEviction{
		accessOrder:  list.New(),
		orderNodes:   make(map[int]*list.Element),
		operationLog: make([]string, 0),
		debugEnabled: false, // 可以设置为true启用调试
	}
}

func (d *DebugLRUEviction) logOperation(operation string) {
	d.operationLog = append(d.operationLog, operation)
	if d.debugEnabled {
		fmt.Printf("[LRU Debug] %s\n", operation)
	}

	// 保持日志大小合理
	if len(d.operationLog) > 1000 {
		d.operationLog = d.operationLog[500:] // 保留后500条
	}
}

func (d *DebugLRUEviction) OnAdd(blockID int) {
	if element, exists := d.orderNodes[blockID]; exists {
		// 如果block已存在，移动到前面（最近使用）
		d.accessOrder.MoveToFront(element)
		d.logOperation(fmt.Sprintf("OnAdd: 移动存在的block %d到队首", blockID))
	} else {
		// 新block添加到前面
		element := d.accessOrder.PushFront(blockID)
		d.orderNodes[blockID] = element
		d.logOperation(fmt.Sprintf("OnAdd: 添加新block %d到队首", blockID))
	}

	d.logOperation(fmt.Sprintf("OnAdd后队列大小: %d", d.accessOrder.Len()))
}

func (d *DebugLRUEviction) Evict(blocks map[int]*Block) int {
	if d.accessOrder.Len() == 0 {
		d.logOperation("Evict: 队列为空，无法淘汰")
		return -1
	}

	// 从队尾获取最久未使用的block
	tail := d.accessOrder.Back()
	if tail == nil {
		d.logOperation("Evict: 队尾为nil，异常情况")
		return -1
	}

	lruBlockID := tail.Value.(int)

	// 验证这个block确实存在于blocks中
	if _, exists := blocks[lruBlockID]; !exists {
		d.logOperation(fmt.Sprintf("Evict: 警告！队列中的block %d不在blocks map中", lruBlockID))
		// 清理无效的队列项
		d.accessOrder.Remove(tail)
		delete(d.orderNodes, lruBlockID)
		return d.Evict(blocks) // 递归尝试下一个
	}

	// 从队列中移除
	d.accessOrder.Remove(tail)
	delete(d.orderNodes, lruBlockID)

	d.logOperation(fmt.Sprintf("Evict: 淘汰LRU block %d，队列剩余: %d", lruBlockID, d.accessOrder.Len()))

	return lruBlockID
}

func (d *DebugLRUEviction) UpdateOnAccess(block *Block) {
	blockID := block.HashID

	if element, exists := d.orderNodes[blockID]; exists {
		// 移动到队首（最近使用）
		d.accessOrder.MoveToFront(element)
		d.logOperation(fmt.Sprintf("UpdateOnAccess: 移动block %d到队首", blockID))
	} else {
		// 这种情况不应该发生，因为OnAdd应该已经处理了
		element := d.accessOrder.PushFront(blockID)
		d.orderNodes[blockID] = element
		d.logOperation(fmt.Sprintf("UpdateOnAccess: 警告！block %d不在队列中，现在添加", blockID))
	}

	// 更新block的访问信息
	block.HitCount++
}

func (d *DebugLRUEviction) GetName() string {
	return "DebugLRU"
}

// PrintDebugInfo 打印调试信息
func (d *DebugLRUEviction) PrintDebugInfo() {
	fmt.Println("\n=== LRU调试信息 ===")
	fmt.Printf("队列长度: %d\n", d.accessOrder.Len())
	fmt.Printf("节点映射大小: %d\n", len(d.orderNodes))

	// 显示队列顺序（从最近到最久）
	fmt.Println("当前LRU队列顺序 (最近 -> 最久):")
	count := 0
	for e := d.accessOrder.Front(); e != nil && count < 10; e = e.Next() {
		fmt.Printf("  %d", e.Value.(int))
		count++
	}
	if d.accessOrder.Len() > 10 {
		fmt.Printf(" ... (还有%d个)", d.accessOrder.Len()-10)
	}
	fmt.Println()

	// 显示最近的操作日志
	fmt.Println("最近的操作日志:")
	start := len(d.operationLog) - 10
	if start < 0 {
		start = 0
	}
	for i := start; i < len(d.operationLog); i++ {
		fmt.Printf("  %s\n", d.operationLog[i])
	}
}

// ValidateConsistency 验证数据一致性
func (d *DebugLRUEviction) ValidateConsistency(blocks map[int]*Block) bool {
	isValid := true

	// 检查：队列中的每个元素都应该在orderNodes中有对应的映射
	for e := d.accessOrder.Front(); e != nil; e = e.Next() {
		blockID := e.Value.(int)
		if mappedElement, exists := d.orderNodes[blockID]; !exists {
			fmt.Printf("❌ 一致性错误: 队列中的block %d在orderNodes中不存在\n", blockID)
			isValid = false
		} else if mappedElement != e {
			fmt.Printf("❌ 一致性错误: block %d的orderNodes映射指向错误的元素\n", blockID)
			isValid = false
		}
	}

	// 检查：orderNodes中的每个映射都应该指向队列中的有效元素
	for blockID, element := range d.orderNodes {
		found := false
		for e := d.accessOrder.Front(); e != nil; e = e.Next() {
			if e == element && e.Value.(int) == blockID {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("❌ 一致性错误: orderNodes中block %d的映射指向无效元素\n", blockID)
			isValid = false
		}
	}

	// 检查：队列长度应该等于orderNodes的大小
	if d.accessOrder.Len() != len(d.orderNodes) {
		fmt.Printf("❌ 一致性错误: 队列长度(%d) != orderNodes大小(%d)\n",
			d.accessOrder.Len(), len(d.orderNodes))
		isValid = false
	}

	if isValid {
		fmt.Println("✅ LRU数据结构一致性检查通过")
	}

	return isValid
}

// CompareWithStandardLRU 与标准LRU实现进行对比测试
func CompareWithStandardLRU() {
	fmt.Println("\n============= LRU实现对比测试 =============")

	// 创建两个LRU实现
	standardLRU := &LRUEviction{
		accessOrder: list.New(),
		orderNodes:  make(map[int]*list.Element),
	}

	debugLRU := NewDebugLRUEviction()
	debugLRU.debugEnabled = true

	// 模拟一系列操作
	blocks := make(map[int]*Block)
	testOperations := []struct {
		op      string
		blockID int
	}{
		{"add", 1}, {"add", 2}, {"add", 3},
		{"access", 1}, {"access", 2},
		{"add", 4}, {"add", 5},
		{"access", 3}, {"access", 1},
	}

	fmt.Println("执行测试操作序列:")
	for _, op := range testOperations {
		fmt.Printf("操作: %s block %d\n", op.op, op.blockID)

		// 确保block存在
		if _, exists := blocks[op.blockID]; !exists {
			blocks[op.blockID] = &Block{HashID: op.blockID}
		}

		if op.op == "add" {
			standardLRU.OnAdd(op.blockID)
			debugLRU.OnAdd(op.blockID)
		} else if op.op == "access" {
			standardLRU.UpdateOnAccess(blocks[op.blockID])
			debugLRU.UpdateOnAccess(blocks[op.blockID])
		}
	}

	// 验证一致性
	fmt.Println("\n验证Debug LRU一致性:")
	debugLRU.ValidateConsistency(blocks)

	// 测试淘汰操作
	fmt.Println("\n测试淘汰操作:")
	for i := 0; i < 3; i++ {
		standardEvict := standardLRU.Evict(blocks)
		debugEvict := debugLRU.Evict(blocks)

		fmt.Printf("淘汰轮次 %d: 标准LRU=%d, Debug LRU=%d", i+1, standardEvict, debugEvict)
		if standardEvict == debugEvict {
			fmt.Println(" ✅")
		} else {
			fmt.Println(" ❌ 不一致!")
		}

		// 从blocks中移除被淘汰的block
		if standardEvict != -1 {
			delete(blocks, standardEvict)
		}
	}

	debugLRU.PrintDebugInfo()
}