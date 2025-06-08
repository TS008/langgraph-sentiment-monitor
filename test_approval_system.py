#!/usr/bin/env python3
"""
神盾系统批准功能测试脚本
"""

import time
from aegis_system.state import create_initial_state
from aegis_system.agents import (
    sentiment_sentinel,
    chief_technical_diagnostician,
    pr_strategist,
    legal_counsel,
    cross_functional_action_coordinator
)

def test_approval_system():
    """测试批准系统的基本功能"""
    print("🧪 开始测试神盾系统批准功能...")
    
    # 创建初始状态
    state = create_initial_state()
    print("✅ 初始状态创建成功")
    
    # 测试舆情哨兵（不需要批准）
    print("\n1. 测试舆情哨兵...")
    state = sentiment_sentinel(state)
    print(f"危机描述: {state.get('crisis_description', 'N/A')}")
    
    # 测试技术诊断官（需要批准）
    print("\n2. 测试首席技术诊断官...")
    try:
        state = chief_technical_diagnostician(state)
        print(f"技术报告: {state.get('technical_report', 'N/A')}")
        print(f"批准请求: {len(state.get('approval_requests', []))} 个")
    except Exception as e:
        print(f"技术诊断官测试失败: {e}")
    
    # 测试公关策略师（需要批准）
    print("\n3. 测试公关策略师...")
    try:
        state = pr_strategist(state)
        print(f"公关策略: {state.get('pr_strategy', 'N/A')}")
        print(f"批准请求: {len(state.get('approval_requests', []))} 个")
    except Exception as e:
        print(f"公关策略师测试失败: {e}")
    
    # 测试法务顾问（需要批准）
    print("\n4. 测试法务顾问...")
    try:
        state = legal_counsel(state)
        print(f"法律审查: {state.get('legal_review', 'N/A')}")
        print(f"批准请求: {len(state.get('approval_requests', []))} 个")
    except Exception as e:
        print(f"法务顾问测试失败: {e}")
    
    # 显示所有批准请求
    print("\n📋 所有批准请求:")
    for i, request in enumerate(state.get('approval_requests', []), 1):
        print(f"{i}. {request['agent']}: {request['explanation']}")
    
    print("\n✅ 批准系统测试完成")
    print("💡 提示: 在Web界面中，这些批准请求会显示在右侧面板，总指挥官可以点击批准或拒绝按钮")

if __name__ == "__main__":
    test_approval_system() 