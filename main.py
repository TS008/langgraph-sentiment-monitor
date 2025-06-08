from aegis_system.graph import create_graph
from aegis_system.state import AegisState, create_initial_state
import warnings

# 抑制所有警告
warnings.filterwarnings("ignore")

def main():
    """
    运行神盾系统演示的主函数。
    """
    print("正在初始化神盾危机管理系统...")
    
    # 创建编译后的图
    app = create_graph()

    # 使用正确的状态初始化函数
    initial_state = create_initial_state()

    print("\n--- 开始危机模拟 ---\n")

    # 运行图
    # 我们使用'stream'来执行图，并可以观察每一步的状态。
    final_state = None
    for s in app.stream(initial_state):
        # 字典的键是刚刚执行的节点的名称。
        node_name = list(s.keys())[0]
        print(f"--- 完成节点：{node_name} ---\n")
        final_state = s[node_name]

    print("\n--- 危机模拟结束 ---")
    
    print("\n\n================ 最终报告 ================")
    print(f"危机描述：{final_state.get('crisis_description')}")
    print("-" * 20)
    print(f"技术报告：{final_state.get('technical_report')}")
    print("-" * 20)
    print(f"公关策略：{final_state.get('pr_strategy')}")
    print("-" * 20)
    print(f"法律审查：{final_state.get('legal_review')}")
    print("-" * 20)
    print(f"人类决策：{final_state.get('human_decision')}")
    print("-" * 20)
    print("完整事件日志：")
    for i, log in enumerate(final_state.get('incident_log', [])):
        print(f"  {i+1}. {log}")
    print("-" * 20)
    print("复盘分析：")
    print(final_state.get('post_mortem_report'))
    print("============================================")


if __name__ == "__main__":
    main() 