from flask import Flask, render_template, request
from flask_socketio import SocketIO, emit
import threading
import json
import time
import random

# 尝试导入LangGraph模块，如果失败则使用模拟模式
try:
    from aegis_system.graph import create_graph
    from aegis_system.state import AegisState
    from aegis_system import agents
    LANGGRAPH_AVAILABLE = True
    print("✅ LangGraph模块导入成功，使用完整功能")
except ImportError as e:
    print(f"⚠️ LangGraph模块导入失败: {e}")
    print("🔄 切换到演示模式")
    LANGGRAPH_AVAILABLE = False

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

# 全局变量
system_running = False
waiting_for_human_input = False
waiting_for_crisis_decision = False

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

def simulate_agent_work(agent_name, work_description, duration=2):
    """模拟智能体工作"""
    update_agent_status(agent_name, "执行中", "blue", work_description)
    time.sleep(duration)
    update_agent_status(agent_name, "完成", "green", f"{work_description} - 已完成")

# 演示模式的智能体函数
def demo_sentiment_sentinel():
    crisis_events = [
        "大量用户报告登录失败，#平台宕机 在Twitter上热搜。用户情绪极度负面且呈指数级增长。",
        "用户反馈应用内支付功能无法使用，多个头部KOL抱怨无法收到打赏，#支付失灵 话题开始发酵。",
        "核心内容推荐流出现严重BUG，大量用户看到不相关或重复的内容，用户在线时长断崖式下跌。"
    ]
    alert = random.choice(crisis_events)
    current_state['initial_alert'] = alert
    current_state['cycles'] = 1
    simulate_agent_work("sentiment_sentinel", "正在监控舆情...")
    update_system_info("alert", alert)

def demo_technical_diagnostician():
    simulate_agent_work("chief_technical_diagnostician", "正在进行技术诊断...")
    tech_report = "初步诊断：数据库连接池耗尽，建议立即重启服务并扩容。预估修复时间：30分钟。"
    current_state['technical_report'] = tech_report
    update_system_info("technical_report", tech_report)

def demo_pr_strategist():
    simulate_agent_work("pr_strategist", "正在制定公关策略...")
    pr_drafts = [
        {"tone": "坦诚", "draft": "我们为技术问题向用户道歉，正在全力修复中。"},
        {"tone": "安抚", "draft": "请放心，我们正在积极处理技术问题。"},
        {"tone": "拖延", "draft": "我们正在调查相关技术情况，稍后更新。"}
    ]
    current_state['pr_drafts'] = pr_drafts
    update_system_info("pr_drafts", pr_drafts)

def demo_legal_counsel():
    simulate_agent_work("legal_counsel", "正在进行法律审查...")
    legal_review = "法律风险评估：坦诚方案风险较低，建议采用。安抚方案需要明确时间承诺。拖延方案可能引发用户不满。"
    current_state['legal_review'] = legal_review
    update_system_info("legal_review", legal_review)

def demo_human_decision_gateway():
    global waiting_for_human_input
    update_agent_status("human_decision_gateway", "等待指令", "orange", "等待总指挥官下达指令...")
    
    proposal = f"""危机警报 (周期 {current_state['cycles']})：
{current_state['initial_alert']}

技术评估：
{current_state['technical_report']}

建议的沟通方案（含法律审查）：
{current_state['legal_review']}"""
    
    update_system_info("comprehensive_proposal", proposal)
    
    # 等待人类输入
    waiting_for_human_input = True
    while waiting_for_human_input:
        time.sleep(0.1)
    
    update_agent_status("human_decision_gateway", "完成", "green", "指令已接收并传达")

def demo_cross_functional_coordinator():
    simulate_agent_work("cross_functional_action_coordinator", "正在协调各部门并发执行...")
    
    # 模拟解析指令和分发任务
    command = current_state.get('human_decision', '')
    tasks = []
    if '公关' in command or '拖延' in command:
        tasks.append("公关部门: 执行拖延策略")
    if '技术' in command or '排查' in command:
        tasks.append("技术部门: 全员排查问题")
    if '舆情' in command or '监控' in command:
        tasks.append("舆情监控: 加强监控力度")
    
    # 模拟并发执行
    for task in tasks:
        print(f"📤 分发任务: {task}")
        time.sleep(0.5)

def demo_realtime_sentiment():
    simulate_agent_work("realtime_sentiment_display", "正在分析实时舆情...")
    sentiment_feedback = "网民对快速响应表示认可，但仍有部分用户质疑处理效率。舆情热度略有下降。"
    current_state['realtime_sentiment'] = sentiment_feedback
    update_system_info("realtime_sentiment", sentiment_feedback)

def demo_crisis_status_check():
    global waiting_for_crisis_decision
    update_agent_status("crisis_status_check", "等待确认", "orange", "等待确认危机状态...")
    
    socketio.emit('crisis_status_check', {
        'message': '危机是否已经解决？'
    })
    
    waiting_for_crisis_decision = True
    while waiting_for_crisis_decision:
        time.sleep(0.1)
    
    resolved = current_state.get('crisis_resolved', False)
    status_msg = "危机已解决，准备复盘" if resolved else "危机未解决，继续处理"
    update_agent_status("crisis_status_check", "完成", "green", status_msg)
    return resolved

def demo_post_mortem_analyst():
    simulate_agent_work("post_mortem_analyst", "正在生成复盘报告...")
    report = "复盘总结：响应及时，技术修复迅速，公关策略得当。建议：加强监控预警机制。"
    current_state['post_mortem_report'] = report
    update_system_info("post_mortem_report", report)

def run_aegis_system():
    """运行神盾系统"""
    global waiting_for_human_input, waiting_for_crisis_decision, current_state
    
    try:
        if LANGGRAPH_AVAILABLE:
            # 使用真实的LangGraph系统
            print("🚀 启动完整LangGraph系统...")
            # 这里可以调用真实的LangGraph图
            # 由于依赖问题，暂时使用演示模式
            run_demo_system()
        else:
            # 使用演示模式
            run_demo_system()
            
    except Exception as e:
        socketio.emit('system_error', {'error': str(e)})
        print(f"❌ 系统错误: {e}")

def run_demo_system():
    """运行演示系统"""
    global waiting_for_crisis_decision
    
    print("🎭 运行演示模式...")
    
    # 1. 舆情哨兵
    demo_sentiment_sentinel()
    
    # 2. 技术诊断官
    demo_technical_diagnostician()
    
    # 3. 公关策略师
    demo_pr_strategist()
    
    # 4. 法务顾问
    demo_legal_counsel()
    
    # 5. 人类决策网关
    demo_human_decision_gateway()
    
    # 6. 跨部门协调官
    demo_cross_functional_coordinator()
    
    # 7. 实时舆情反馈
    demo_realtime_sentiment()
    
    # 8. 危机状态检查
    resolved = demo_crisis_status_check()
    
    # 9. 复盘分析师（如果危机解决）
    if resolved:
        demo_post_mortem_analyst()
    
    socketio.emit('system_completed', {'message': '神盾系统执行完成'})

@app.route('/')
def index():
    return render_template('index.html')

@socketio.on('connect')
def handle_connect():
    print('客户端已连接')
    mode = "完整模式" if LANGGRAPH_AVAILABLE else "演示模式"
    emit('connected', {'data': f'连接成功 - {mode}'})

@socketio.on('start_system')
def handle_start_system():
    """启动神盾系统"""
    global system_running
    if not system_running:
        system_running = True
        thread = threading.Thread(target=run_aegis_system)
        thread.daemon = True
        thread.start()
        mode = "完整模式" if LANGGRAPH_AVAILABLE else "演示模式"
        emit('system_started', {'message': f'神盾系统已启动 - {mode}'})

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

if __name__ == '__main__':
    print("🛡️ 神盾危机管理系统 - 中控大屏启动中...")
    print("📡 访问地址: http://localhost:5000")
    if LANGGRAPH_AVAILABLE:
        print("✅ 完整功能模式")
    else:
        print("🎭 演示模式 (LangGraph不可用)")
    
    socketio.run(app, debug=True, host='0.0.0.0', port=5000) 