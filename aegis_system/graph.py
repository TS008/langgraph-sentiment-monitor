from langgraph.graph import StateGraph, END
from aegis_system.state import AegisState
from aegis_system.agents import (
    sentiment_sentinel,
    chief_technical_diagnostician,
    pr_strategist,
    legal_counsel,
    human_decision_gateway,
    cross_functional_action_coordinator,
    crisis_status_check,
    post_mortem_analyst,
    realtime_sentiment_feedback,
    realtime_sentiment_display
)

def create_graph():
    """
    (v3.0) 创建神盾系统的完整状态图。
    """
    workflow = StateGraph(AegisState)

    # 1. Add nodes for each agent
    workflow.add_node("sentiment_sentinel", sentiment_sentinel)
    workflow.add_node("technical_diagnostician", chief_technical_diagnostician)
    workflow.add_node("pr_strategist", pr_strategist)
    workflow.add_node("legal_counsel", legal_counsel)
    workflow.add_node("decision_gateway", human_decision_gateway)
    workflow.add_node("action_coordinator", cross_functional_action_coordinator)
    workflow.add_node("realtime_sentiment_feedback", realtime_sentiment_feedback)
    workflow.add_node("realtime_sentiment_display", realtime_sentiment_display)
    workflow.add_node("status_check", crisis_status_check)
    workflow.add_node("post_mortem_analyst", post_mortem_analyst)

    # 2. Define the graph's execution flow
    workflow.set_entry_point("sentiment_sentinel")
    
    # Analysis phase
    workflow.add_edge("sentiment_sentinel", "technical_diagnostician")
    workflow.add_edge("technical_diagnostician", "pr_strategist")
    workflow.add_edge("pr_strategist", "legal_counsel")
    
    # Decision and Execution phase
    workflow.add_edge("legal_counsel", "decision_gateway")
    workflow.add_edge("decision_gateway", "action_coordinator")
    workflow.add_edge("action_coordinator", "realtime_sentiment_feedback")
    workflow.add_edge("realtime_sentiment_feedback", "realtime_sentiment_display")
    workflow.add_edge("realtime_sentiment_display", "status_check")

    # The final node goes to END
    workflow.add_edge("post_mortem_analyst", END)

    # 3. Add conditional logic for looping or ending
    def should_continue(state: AegisState):
        """
        Determines whether to continue the loop or end the process.
        """
        # 检查人类指令或危机状态
        human_command = state.get('human_command', '').lower()
        crisis_resolved = state.get('crisis_resolved', False)
        
        if crisis_resolved or "危机已解除" in human_command or "结束" in human_command:
            print("✅ 危机已解决，流程结束。")
            return "end"
        else:
            # 增加循环计数
            current_cycles = state.get("cycles", 0)
            state["cycles"] = current_cycles + 1
            print(f"🔄 危机尚未解决，准备进入第 {state['cycles']} 轮。")
            return "continue"

    workflow.add_conditional_edges(
        "status_check",
        should_continue,
        {
            # If not resolved, loop back to the beginning
            "continue": "sentiment_sentinel",
            # If resolved, proceed to post-mortem analysis
            "end": "post_mortem_analyst",
        },
    )

    # 4. Compile the graph
    print("正在编译神盾系统v3.0图...")
    aegis_graph = workflow.compile()
    print("图编译成功。")
    return aegis_graph 