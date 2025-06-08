from flask import Flask, render_template, request
from flask_socketio import SocketIO, emit
import threading
import json
import time
from aegis_system.graph import create_graph
from aegis_system.state import AegisState

app = Flask(__name__)
app.config['SECRET_KEY'] = 'aegis_command_center_2024'
socketio = SocketIO(app, cors_allowed_origins="*")

# 全局状态管理
current_state = {
    "initial_alert": None,
    "cycles": 0,
    "crisis_resolved": False,
    "technical_report": None,
    "pr_drafts": None,
    "legal_review": None,
    "comprehensive_proposal": None,
    "human_decision": None,
    "realtime_sentiment": None,
    "latest_actions": None,
    "action_log": [],
    "post_mortem_report": None
}

# 智能体状态管理
agent_status = {
    "sentiment_sentinel": {"status": "待机", "color": "gray", "message": ""},
    "chief_technical_diagnostician": {"status": "待机", "color": "gray", "message": ""},
    "pr_strategist": {"status": "待机", "color": "gray", "message": ""},
    "legal_counsel": {"status": "待机", "color": "gray", "message": ""},
    "human_decision_gateway": {"status": "待机", "color": "gray", "message": ""},
    "cross_functional_action_coordinator": {"status": "待机", "color": "gray", "message": ""},
    "realtime_sentiment_display": {"status": "待机", "color": "gray", "message": ""},
    "crisis_status_check": {"status": "待机", "color": "gray", "message": ""},
    "post_mortem_analyst": {"status": "待机", "color": "gray", "message": ""}
}

# 创建神盾系统图
aegis_graph = None
graph_thread = None
waiting_for_human_input = False

def update_agent_status(agent_name, status, color, message=""):
    """更新智能体状态并通知前端"""
    agent_status[agent_name] = {
        "status": status,
        "color": color,
        "message": message
    }
    socketio.emit('agent_status_update', {
        'agent': agent_name,
        'status': status,
        'color': color,
        'message': message
    })

def update_system_info(info_type, content):
    """更新系统信息并通知前端"""
    socketio.emit('system_info_update', {
        'type': info_type,
        'content': content
    })

class WebUIAgentWrapper:
    """智能体包装器，用于Web UI状态更新"""
    
    @staticmethod
    def sentiment_sentinel(state):
        update_agent_status("sentiment_sentinel", "执行中", "blue", "正在监控舆情...")
        
        from aegis_system.agents import sentiment_sentinel
        result = sentiment_sentinel(state)
        
        update_agent_status("sentiment_sentinel", "完成", "green", f"周期 {result.get('cycles', 0)} 警报已发布")
        update_system_info("alert", result.get('initial_alert', ''))
        
        return result
    
    @staticmethod
    def chief_technical_diagnostician(state):
        update_agent_status("chief_technical_diagnostician", "执行中", "blue", "正在进行技术诊断...")
        
        from aegis_system.agents import chief_technical_diagnostician
        result = chief_technical_diagnostician(state)
        
        update_agent_status("chief_technical_diagnostician", "完成", "green", "技术诊断报告已生成")
        update_system_info("technical_report", result.get('technical_report', ''))
        
        return result
    
    @staticmethod
    def pr_strategist(state):
        update_agent_status("pr_strategist", "执行中", "blue", "正在制定公关策略...")
        
        from aegis_system.agents import pr_strategist
        result = pr_strategist(state)
        
        update_agent_status("pr_strategist", "完成", "green", f"已生成 {len(result.get('pr_drafts', []))} 个公关草案")
        update_system_info("pr_drafts", result.get('pr_drafts', []))
        
        return result
    
    @staticmethod
    def legal_counsel(state):
        update_agent_status("legal_counsel", "执行中", "blue", "正在进行法律审查...")
        
        from aegis_system.agents import legal_counsel
        result = legal_counsel(state)
        
        update_agent_status("legal_counsel", "完成", "green", "法律审查已完成")
        update_system_info("legal_review", result.get('legal_review', ''))
        
        return result
    
    @staticmethod
    def human_decision_gateway(state):
        global waiting_for_human_input
        update_agent_status("human_decision_gateway", "等待指令", "orange", "等待总指挥官下达指令...")
        
        # 发送综合提案到前端
        proposal = f"""
        危机警报 (周期 {state.get('cycles', 0)-1})：
        {state.get('initial_alert', '')}

        技术评估：
        {state.get('technical_report', '')}

        建议的沟通方案（含法律审查）：
        {state.get('legal_review', '')}
        """
        
        update_system_info("comprehensive_proposal", proposal)
        
        # 等待人类输入
        waiting_for_human_input = True
        while waiting_for_human_input:
            time.sleep(0.1)
        
        update_agent_status("human_decision_gateway", "完成", "green", "指令已接收并传达")
        
        return {
            "comprehensive_proposal": proposal,
            "human_decision": current_state.get('human_decision', ''),
            "action_log": state.get('action_log', []) + [f"总指挥官指令：{current_state.get('human_decision', '')}"]
        }
    
    @staticmethod
    def cross_functional_action_coordinator(state):
        update_agent_status("cross_functional_action_coordinator", "执行中", "blue", "正在协调各部门并发执行...")
        
        from aegis_system.agents import cross_functional_action_coordinator
        result = cross_functional_action_coordinator(state)
        
        update_agent_status("cross_functional_action_coordinator", "完成", "green", "各部门任务并发执行完成")
        
        return result
    
    @staticmethod
    def realtime_sentiment_display(state):
        update_agent_status("realtime_sentiment_display", "执行中", "blue", "正在分析实时舆情...")
        
        from aegis_system.agents import realtime_sentiment_display
        result = realtime_sentiment_display(state)
        
        update_agent_status("realtime_sentiment_display", "完成", "green", "实时舆情反馈已更新")
        update_system_info("realtime_sentiment", result.get('realtime_sentiment', ''))
        
        return result
    
    @staticmethod
    def crisis_status_check(state):
        update_agent_status("crisis_status_check", "等待确认", "orange", "等待确认危机状态...")
        
        # 询问危机是否解决
        socketio.emit('crisis_status_check', {
            'message': '危机是否已经解决？'
        })
        
        # 等待前端响应
        global waiting_for_crisis_decision
        waiting_for_crisis_decision = True
        while waiting_for_crisis_decision:
            time.sleep(0.1)
        
        resolved = current_state.get('crisis_resolved', False)
        status_msg = "危机已解决，准备复盘" if resolved else "危机未解决，继续处理"
        update_agent_status("crisis_status_check", "完成", "green", status_msg)
        
        return {"crisis_resolved": resolved}
    
    @staticmethod
    def post_mortem_analyst(state):
        update_agent_status("post_mortem_analyst", "执行中", "blue", "正在生成复盘报告...")
        
        from aegis_system.agents import post_mortem_analyst
        result = post_mortem_analyst(state)
        
        update_agent_status("post_mortem_analyst", "完成", "green", "复盘报告已生成")
        update_system_info("post_mortem_report", result.get('post_mortem_report', ''))
        
        return result

def run_aegis_system():
    """在后台线程中运行神盾系统"""
    global aegis_graph, current_state
    
    # 创建包装后的图
    from langgraph.graph import StateGraph, END
    
    workflow = StateGraph(AegisState)
    
    # 添加包装后的节点
    workflow.add_node("sentiment_sentinel", WebUIAgentWrapper.sentiment_sentinel)
    workflow.add_node("chief_technical_diagnostician", WebUIAgentWrapper.chief_technical_diagnostician)
    workflow.add_node("pr_strategist", WebUIAgentWrapper.pr_strategist)
    workflow.add_node("legal_counsel", WebUIAgentWrapper.legal_counsel)
    workflow.add_node("human_decision_gateway", WebUIAgentWrapper.human_decision_gateway)
    workflow.add_node("cross_functional_action_coordinator", WebUIAgentWrapper.cross_functional_action_coordinator)
    workflow.add_node("realtime_sentiment_display", WebUIAgentWrapper.realtime_sentiment_display)
    workflow.add_node("crisis_status_check", WebUIAgentWrapper.crisis_status_check)
    workflow.add_node("post_mortem_analyst", WebUIAgentWrapper.post_mortem_analyst)
    
    # 定义边
    workflow.set_entry_point("sentiment_sentinel")
    workflow.add_edge("sentiment_sentinel", "chief_technical_diagnostician")
    workflow.add_edge("chief_technical_diagnostician", "pr_strategist")
    workflow.add_edge("pr_strategist", "legal_counsel")
    workflow.add_edge("legal_counsel", "human_decision_gateway")
    workflow.add_edge("human_decision_gateway", "cross_functional_action_coordinator")
    workflow.add_edge("cross_functional_action_coordinator", "realtime_sentiment_display")
    workflow.add_edge("realtime_sentiment_display", "crisis_status_check")
    workflow.add_edge("post_mortem_analyst", END)
    
    def decide_if_crisis_is_over(state):
        if state["crisis_resolved"]:
            return "post_mortem_analyst"
        else:
            return "sentiment_sentinel"
    
    workflow.add_conditional_edges(
        "crisis_status_check",
        decide_if_crisis_is_over,
        {
            "post_mortem_analyst": "post_mortem_analyst",
            "sentiment_sentinel": "sentiment_sentinel",
        }
    )
    
    aegis_graph = workflow.compile()
    
    # 运行系统
    try:
        result = aegis_graph.invoke(current_state)
        current_state.update(result)
    except Exception as e:
        socketio.emit('system_error', {'error': str(e)})

@app.route('/')
def index():
    return render_template('index.html')

@socketio.on('connect')
def handle_connect():
    print('客户端已连接')
    emit('connected', {'data': '连接成功'})

@socketio.on('start_system')
def handle_start_system():
    """启动神盾系统"""
    global graph_thread
    if graph_thread is None or not graph_thread.is_alive():
        graph_thread = threading.Thread(target=run_aegis_system)
        graph_thread.daemon = True
        graph_thread.start()
        emit('system_started', {'message': '神盾系统已启动'})

@socketio.on('human_command')
def handle_human_command(data):
    """处理人类指令"""
    global waiting_for_human_input
    command = data.get('command', '')
    current_state['human_decision'] = command
    waiting_for_human_input = False
    emit('command_received', {'message': f'指令已接收: {command}'})

@socketio.on('crisis_decision')
def handle_crisis_decision(data):
    """处理危机状态决策"""
    global waiting_for_crisis_decision
    resolved = data.get('resolved', False)
    current_state['crisis_resolved'] = resolved
    waiting_for_crisis_decision = False
    emit('crisis_decision_received', {'resolved': resolved})

waiting_for_crisis_decision = False

if __name__ == '__main__':
    socketio.run(app, debug=True, host='0.0.0.0', port=5000) 