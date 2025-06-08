from flask import Flask, render_template, request
from flask_socketio import SocketIO, emit
import threading
import json
import time
import random
import os
from dotenv import load_dotenv
import queue
from datetime import datetime
import openai
from aegis_system.memory import memory_instance

# 尝试导入LangGraph相关模块
try:
    from aegis_system.state import AegisState
    from aegis_system.graph import create_graph
    from aegis_system.state import create_initial_state
    LANGGRAPH_AVAILABLE = True
    print("✅ LangGraph模块加载成功")
except ImportError as e:
    print(f"⚠️ LangGraph模块导入失败: {e}")
    print("🎭 将使用演示模式")
    LANGGRAPH_AVAILABLE = False

# The .env file is now loaded by the aegis_system package's __init__.py
# load_dotenv() is no longer needed here.

app = Flask(__name__)
app.config['SECRET_KEY'] = 'aegis_command_center_2024'
socketio = SocketIO(app, cors_allowed_origins="*", async_mode='threading')

# 指令队列系统
command_queue = queue.Queue()
command_processing = False

# 全局状态管理
if LANGGRAPH_AVAILABLE:
    current_state = create_initial_state()
else:
    current_state = {
        'cycles': 0,
        'crisis_resolved': False,
        'crisis_description': None,
        'technical_report': None,
        'pr_strategy': None,
        'legal_review': None,
        'human_command': None,
        'parsed_tasks': None,
        'executed_actions': None,
        'incident_log': [],
        'department_execution': {},
        'approval_requests': [],
        'pending_approval': False,
        'action_log': []
    }

# 智能体状态管理
agent_status = {
    "sentiment_sentinel": {"status": "待机", "color": "gray", "message": ""},
    "technical_diagnostician": {"status": "待机", "color": "gray", "message": ""},
    "pr_strategist": {"status": "待机", "color": "gray", "message": ""},
    "legal_counsel": {"status": "待机", "color": "gray", "message": ""},
    "decision_gateway": {"status": "待机", "color": "gray", "message": ""},
    "action_coordinator": {"status": "待机", "color": "gray", "message": ""},
    "realtime_sentiment_feedback": {"status": "待机", "color": "gray", "message": ""},
    "realtime_sentiment_display": {"status": "待机", "color": "gray", "message": ""},
    "status_check": {"status": "待机", "color": "gray", "message": ""},
    "post_mortem_analyst": {"status": "待机", "color": "gray", "message": ""}
}

# 系统信息管理
system_info = {
    "alert": "等待系统启动...",
    "technical_report": "",
    "pr_strategy": "",
    "legal_review": "",
    "comprehensive_proposal": "",
    "human_decision": "",
    "task_distribution": "",
    "executed_actions": "",
    "realtime_sentiment": "",
    "post_mortem_report": "",
    "incident_log": [],
    "approval_requests": []
}

# 全局变量
system_running = False
waiting_for_human_input = False
waiting_for_crisis_decision = False
waiting_for_approval = False
aegis_graph = None
sentiment_thread = None
sentiment_event = threading.Event()

# LLM 调用客户端
llm_client = openai.OpenAI(
    api_key=os.getenv("DEEPSEEK_API_KEY"),
    base_url=os.getenv("DEEPSEEK_BASE_URL")
)

def get_llm_response(prompt):
    """通用LLM调用函数"""
    try:
        response = llm_client.chat.completions.create(
            model=os.getenv("DEEPSEEK_MODEL", "deepseek-chat"),
            messages=[{"role": "user", "content": prompt}],
            max_tokens=150,
            temperature=0.7,
        )
        content = response.choices[0].message.content.strip()
        # 确保内容不超过50个字
        return content[:50]
    except Exception as e:
        print(f"❌ LLM调用失败: {e}")
        return f"LLM响应错误，请检查API配置。({str(e)[:30]})"

def update_agent_status(agent_name, status, color, message=""):
    """更新智能体状态并通知前端"""
    agent_status[agent_name] = {
        "status": status,
        "color": color,
        "message": message
    }
    print(f"🔄 更新智能体状态: {agent_name} -> {status} ({color}) - {message}")
    socketio.emit('agent_status_update', {
        'agent': agent_name,
        'status': status,
        'color': color,
        'message': message
    })

def update_system_info(info_type, content):
    """更新系统信息并通过WebSocket发送给前端"""
    system_info[info_type] = content
    socketio.emit('system_info_update', {
        'type': info_type,
        'content': content
    })
    
    # 特殊处理批准请求
    if info_type == 'approval_requests':
        update_approval_requests(content)

def update_task_distribution(tasks):
    """更新任务分发状态"""
    if isinstance(tasks, dict) and 'tasks' in tasks:
        content = "任务分发:\n"
        for i, task in enumerate(tasks['tasks'], 1):
            content += f"{i}. {task.get('dept', '未知部门')}: {task.get('action', '未知任务')}\n"
    else:
        content = str(tasks)
    
    update_system_info("task_distribution", content)

def update_department_execution(dept, status, result=""):
    """更新部门执行状态"""
    if dept not in current_state['department_execution']:
        current_state['department_execution'][dept] = {}
    
    current_state['department_execution'][dept]['status'] = status
    if result:
        current_state['department_execution'][dept]['result'] = result
    
    # 格式化显示内容
    content = ""
    for department, info in current_state['department_execution'].items():
        status_text = info.get('status', '未知')
        result_text = info.get('result', '')
        content += f"{department}: {status_text}\n"
        if result_text:
            content += f"  结果: {result_text}\n"
        content += "\n"
    
    update_system_info("department_execution", content)

def process_immediate_command(command):
    """立即处理总指挥官指令"""
    print(f"⚡ 立即处理指令: {command}")
    
    # 更新指挥官状态
    update_agent_status("human_decision_gateway", "执行中", "blue", f"正在处理指令: {command}")
    
    # 激活跨部门协调官处理指令
    update_agent_status("cross_functional_action_coordinator", "执行中", "blue", "正在解析和分发指令...")
    
    # 模拟指令解析和任务分发
    time.sleep(0.5)
    
    # 解析指令并生成任务
    tasks = parse_command_to_tasks(command)
    update_task_distribution(tasks)
    
    # 并发执行任务
    execute_tasks_concurrently(tasks)
    
    # 生成实时舆情反馈
    generate_realtime_sentiment_feedback()
    
    # 更新状态
    update_agent_status("human_decision_gateway", "完成", "green", "指令处理完成")
    update_agent_status("cross_functional_action_coordinator", "完成", "green", f"已完成 {len(tasks)} 项任务")
    
    # 记录到行动日志
    if 'action_log' not in current_state:
        current_state['action_log'] = []
    current_state['action_log'].append({
        'timestamp': datetime.now().strftime("%H:%M:%S"),
        'command': command,
        'tasks': len(tasks),
        'status': '已完成'
    })
    
    # 通知前端指令已处理
    socketio.emit('command_processed', {
        'command': command,
        'tasks_count': len(tasks),
        'timestamp': datetime.now().strftime("%H:%M:%S")
    })

def parse_command_to_tasks(command):
    """解析指令为具体任务"""
    tasks = []
    command_lower = command.lower()
    
    # 技术相关指令
    if any(keyword in command_lower for keyword in ['技术', '修复', '系统', '服务器', '数据库', '网络']):
        tasks.append({
            'dept': '技术部门',
            'action': f'执行技术指令: {command}',
            'priority': 'high'
        })
    
    # 公关相关指令
    if any(keyword in command_lower for keyword in ['公关', '声明', '媒体', '用户', '道歉', '解释']):
        tasks.append({
            'dept': '公关部门',
            'action': f'执行公关指令: {command}',
            'priority': 'high'
        })
    
    # 法务相关指令
    if any(keyword in command_lower for keyword in ['法务', '合规', '法律', '风险', '责任']):
        tasks.append({
            'dept': '法务部门',
            'action': f'执行法务指令: {command}',
            'priority': 'medium'
        })
    
    # 运营相关指令
    if any(keyword in command_lower for keyword in ['运营', '用户', '补偿', '客服', '通知']):
        tasks.append({
            'dept': '运营部门',
            'action': f'执行运营指令: {command}',
            'priority': 'medium'
        })
    
    # 如果没有匹配到具体部门，创建通用任务
    if not tasks:
        tasks.append({
            'dept': '综合协调',
            'action': f'执行综合指令: {command}',
            'priority': 'medium'
        })
    
    return tasks

def execute_tasks_concurrently(tasks):
    """并发执行任务"""
    import concurrent.futures
    
    def execute_single_task(task):
        dept = task['dept']
        action = task['action']
        
        # 更新部门状态为执行中
        update_department_execution(dept, "执行中")
        
        # 模拟任务执行时间
        execution_time = random.uniform(1, 3)
        time.sleep(execution_time)
        
        # 生成执行结果
        result = generate_task_result(dept, action)
        
        # 更新部门状态为完成
        update_department_execution(dept, "完成", result)
        
        return {'dept': dept, 'result': result}
    
    # 使用线程池并发执行
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        futures = [executor.submit(execute_single_task, task) for task in tasks]
        results = [future.result() for future in concurrent.futures.as_completed(futures)]
    
    return results

def generate_task_result(dept, action):
    """生成任务执行结果"""
    results = {
        '技术部门': [
            "系统性能优化完成，响应时间提升30%",
            "紧急修复部署完成，服务已恢复正常",
            "数据库优化完成，查询效率显著提升",
            "网络配置调整完成，连接稳定性增强"
        ],
        '公关部门': [
            "官方声明已发布，用户反馈积极",
            "媒体沟通完成，负面报道得到澄清",
            "用户安抚措施已实施，满意度回升",
            "危机公关策略执行完成，品牌形象稳定"
        ],
        '法务部门': [
            "法律风险评估完成，合规性确认",
            "责任界定清晰，法律文件准备就绪",
            "合规审查通过，操作符合法规要求",
            "法律保护措施已激活，风险可控"
        ],
        '运营部门': [
            "用户补偿方案已启动，执行顺利",
            "客服团队已就位，用户问题快速响应",
            "运营数据监控正常，指标稳定",
            "用户通知已发送，覆盖率100%"
        ],
        '综合协调': [
            "跨部门协调完成，各项工作有序推进",
            "资源调配到位，执行效率提升",
            "综合方案实施完成，效果良好",
            "统筹安排优化，整体运行顺畅"
        ]
    }
    
    return random.choice(results.get(dept, ["任务执行完成，效果良好"]))

def generate_realtime_sentiment_feedback():
    """生成实时舆情反馈"""
    update_agent_status("realtime_sentiment_feedback", "执行中", "blue", "正在分析实时舆情...")
    time.sleep(0.5)
    
    # 根据执行的任务生成相应的舆情反馈
    executed_depts = list(current_state['department_execution'].keys())
    
    sentiment_options = [
        "用户对快速响应表示认可，舆情热度明显下降",
        "网民对处理效率表示满意，正面评价增加",
        "社交媒体讨论趋于理性，负面情绪缓解",
        "用户信心逐步恢复，品牌形象得到维护",
        "媒体报道转向正面，危机影响得到控制"
    ]
    
    sentiment_feedback = random.choice(sentiment_options)
    current_state['realtime_sentiment'] = sentiment_feedback
    update_system_info("realtime_sentiment", sentiment_feedback)
    update_agent_status("realtime_sentiment_feedback", "完成", "green", "实时舆情分析完成")

def command_processor():
    """指令处理器线程 - 持续监听和处理指令"""
    global command_processing
    command_processing = True
    
    print("🎯 指令处理器启动 - 总指挥官可随时下达命令")
    
    while command_processing and system_running:
        try:
            # 检查是否有新指令（非阻塞）
            try:
                command_data = command_queue.get(timeout=0.1)
                command = command_data.get('command', '')
                command_type = command_data.get('type', 'immediate')
                
                if command.strip() and command_type == 'immediate':
                    print(f"📨 处理立即指令: {command}")
                    # 在独立线程中处理，不阻塞主流程
                    threading.Thread(
                        target=process_immediate_command, 
                        args=(command,),
                        daemon=True
                    ).start()
                
                command_queue.task_done()
                
            except queue.Empty:
                # 没有新指令，继续监听
                continue
                
        except Exception as e:
            print(f"❌ 指令处理错误: {e}")
            time.sleep(0.1)
    
    print("🛑 指令处理器已停止")

def run_real_aegis_system():
    """运行真实的神盾系统"""
    global waiting_for_crisis_decision, system_running, aegis_graph
    
    print("🛡️ 启动真实神盾系统...")
    
    try:
        # 初始化LangGraph
        if aegis_graph is None:
            aegis_graph = create_graph()
            print("✅ LangGraph图创建成功")
        
        while system_running:
            # 使用正确的状态初始化
            initial_state = create_initial_state()
            initial_state["cycles"] = current_state.get('cycles', 0)
            initial_state["crisis_resolved"] = False
            
            print(f"🔄 开始第 {initial_state['cycles'] + 1} 轮处理...")
            
            # 处理每个步骤的状态更新
            for step in aegis_graph.stream(initial_state):
                if not system_running:
                    break
                    
                print(f"📊 LangGraph步骤: {list(step.keys())}")
                
                for node_name, node_state in step.items():
                    if not system_running:
                        break
                        
                    print(f"🔍 处理节点: {node_name}")
                    print(f"📋 节点状态: {node_state}")
                    
                    # 更新智能体状态
                    update_agent_status(node_name, "执行中", "blue", f"正在处理第{initial_state['cycles']+1}轮...")
                    
                    # 根据节点类型更新相应的系统信息
                    if node_name == "sentiment_sentinel":
                        if 'crisis_description' in node_state:
                            update_system_info("alert", node_state['crisis_description'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "technical_diagnostician":
                        if 'technical_report' in node_state:
                            update_system_info("technical_report", node_state['technical_report'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "pr_strategist":
                        if 'pr_strategy' in node_state:
                            update_system_info("pr_strategy", node_state['pr_strategy'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "legal_counsel":
                        if 'legal_review' in node_state:
                            update_system_info("legal_review", node_state['legal_review'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "decision_gateway":
                        if 'parsed_tasks' in node_state:
                            update_system_info("parsed_tasks", node_state['parsed_tasks'])
                        if 'human_command' in node_state:
                            update_system_info("human_decision", node_state['human_command'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "action_coordinator":
                        if 'executed_actions' in node_state:
                            update_system_info("executed_actions", node_state['executed_actions'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "realtime_sentiment_feedback":
                        if 'realtime_sentiment' in node_state:
                            update_system_info("realtime_sentiment", node_state['realtime_sentiment'])
                        if 'incident_log' in node_state:
                            update_system_info("incident_log", node_state['incident_log'])
                            
                    elif node_name == "post_mortem_analyst":
                        if 'post_mortem_report' in node_state:
                            update_system_info("post_mortem_report", node_state['post_mortem_report'])
                    
                    # 更新全局状态
                    current_state.update(node_state)
                    
                    # 标记智能体完成
                    update_agent_status(node_name, "完成", "green", "处理完成")
                    
                    time.sleep(0.5)  # 短暂延迟，让用户看到状态变化
                
            # 检查危机状态
            if current_state.get('crisis_resolved', False):
                socketio.emit('system_completed', {'message': '神盾系统执行完成 - 危机已解决'})
                print("✅ 神盾系统执行完成 - 危机已解决")
                break
            else:
                current_state['cycles'] = current_state.get('cycles', 0) + 1
                current_state['department_execution'] = {}
                print(f"🔄 危机未解决，开始第 {current_state['cycles'] + 1} 轮处理...")
                
                # 重置智能体状态
                for agent_name in agent_status.keys():
                    update_agent_status(agent_name, "待机", "gray", "")
                
                time.sleep(1)
        
    except Exception as e:
        print(f"❌ 真实系统执行错误: {e}")
        import traceback
        traceback.print_exc()
        print("🎭 切换到演示模式...")
        run_demo_system()
    finally:
        system_running = False

def demo_sentiment_sentinel():
    print("🔍 启动舆情哨兵...")
    
    if current_state['cycles'] == 1:
        # 第一轮：生成初始危机
        crisis_events = [
            "大量用户报告登录失败，#平台宕机 在Twitter上热搜。用户情绪极度负面且呈指数级增长。",
            "用户反馈应用内支付功能无法使用，多个头部KOL抱怨无法收到打赏，#支付失灵 话题开始发酵。",
            "核心内容推荐流出现严重BUG，大量用户看到不相关或重复的内容，用户在线时长断崖式下跌。"
        ]
        alert = random.choice(crisis_events)
        current_state['crisis_description'] = alert
    else:
        # 后续轮次：危机升级
        escalation_scenarios = [
            f"危机升级！媒体开始报道此次事件，#平台危机 登上热搜榜首。监管部门表示关注。",
            f"情况恶化！竞争对手趁机发声，用户开始大规模转移到其他平台。股价出现波动。",
            f"危机扩散！多家媒体跟进报道，用户发起集体投诉。公司声誉面临严重威胁。"
        ]
        escalation = random.choice(escalation_scenarios)
        current_state['crisis_description'] = f"{current_state['crisis_description']}\n\n【第{current_state['cycles']}轮升级】\n{escalation}"
    
    update_agent_status("sentiment_sentinel", "执行中", "blue", "正在监控舆情...")
    time.sleep(1)  # 短暂延迟
    update_system_info("alert", current_state['crisis_description'])
    update_agent_status("sentiment_sentinel", "完成", "green", f"舆情监控完成 - 第{current_state['cycles']}轮")

def demo_technical_diagnostician():
    print("🔧 启动技术诊断官...")
    update_agent_status("technical_diagnostician", "执行中", "blue", "正在生成技术诊断...")
    prompt = f"针对以下危机警报，生成一份不超过50字的核心技术诊断报告：'{current_state['crisis_description']}'"
    tech_report = get_llm_response(prompt)
    current_state['technical_report'] = tech_report
    update_system_info("technical_report", tech_report)
    update_agent_status("technical_diagnostician", "完成", "green", "技术诊断完成")

def demo_pr_strategist():
    print("📢 启动公关策略师...")
    update_agent_status("pr_strategist", "执行中", "blue", "正在制定公关策略...")
    prompt = f"根据危机'{current_state['crisis_description']}'和技术报告'{current_state['technical_report']}'，提供一份不超过50字的公关核心策略。"
    pr_strategy = get_llm_response(prompt)
    current_state['pr_strategy'] = pr_strategy
    update_system_info("pr_strategy", pr_strategy)
    update_agent_status("pr_strategist", "完成", "green", "公关策略制定完成")

def demo_legal_counsel():
    print("⚖️ 启动法务顾问...")
    update_agent_status("legal_counsel", "执行中", "blue", "正在进行法律审查...")
    prompt = f"基于危机'{current_state['crisis_description']}'和公关策略'{current_state['pr_strategy']}'，简要评估法律风险（50字内）。"
    legal_review = get_llm_response(prompt)
    current_state['legal_review'] = legal_review
    update_system_info("legal_review", legal_review)
    update_agent_status("legal_counsel", "完成", "green", "法律审查完成")

def demo_human_decision_gateway():
    """人类决策网关，内置20秒超时的AI数字分身"""
    print("👑 启动人类决策网关...")
    global waiting_for_human_input
    
    proposal = f"""危机警报: {current_state['crisis_description']}
技术诊断: {current_state['technical_report']}
公关策略: {current_state['pr_strategy']}
法律评估: {current_state['legal_review']}"""
    update_system_info("comprehensive_proposal", proposal)
    
    update_agent_status("decision_gateway", "等待指令", "orange", "等待总指挥官下达指令 (20秒)...")
    
    waiting_for_human_input = True
    print("⏳ 等待总指挥官指令... 20秒后AI数字分身将接管")

    # 等待20秒或直到人类输入
    for _ in range(200): # 200 * 0.1s = 20s
        if not waiting_for_human_input:
            print("👤 总指挥官已介入决策。")
            update_agent_status("decision_gateway", "完成", "green", "总指挥官指令已接收")
            return
        time.sleep(0.1)

    # 如果20秒后仍然在等待，AI数字分身介入
    if waiting_for_human_input:
        print("🤖 20秒无响应，AI数字分身启动决策...")
        update_agent_status("decision_gateway", "执行中", "blue", "🤖 AI分身已接管，决策中...")
        
        # 步骤1：从记忆中提取相关经验
        relevant_experiences = memory_instance.retrieve_experiences(current_state['crisis_description'])
        print(f"🤖 AI分身正在参考以下经验:\n{relevant_experiences}")
        
        # 步骤2：基于经验进行决策
        prompt = f"""你是公司的AI数字分身CEO。以公司利益最大化为核心，根据以下信息和历史经验，给出一个全局统筹的、不超过50字的行动指令:
        
        【当前提案】:
        {proposal}
        
        【历史经验参考】:
        {relevant_experiences}
        
        你的决策指令是:
        """
        decision = get_llm_response(prompt)
        current_state['human_command'] = f"【AI决策】{decision}"
        print(f"🤖 AI数字分身决策: {decision}")
        
        update_agent_status("decision_gateway", "完成", "green", "AI分身决策完毕")

    waiting_for_human_input = False

def execute_department_task(dept_name, task_action):
    """模拟部门执行任务"""
    print(f"🏢 {dept_name} 开始执行任务...")
    update_department_execution(dept_name, "执行中")
    
    # 模拟执行时间
    time.sleep(random.uniform(1, 3))
    
    # 生成执行结果
    results = {
        "技术部门": [
            "已重启核心服务器，数据库连接池已扩容至500个连接",
            "技术团队全员到岗，正在进行系统全面检查",
            "已启动备用服务器，确保服务稳定性"
        ],
        "公关部门": [
            "拖延策略公关稿已发布至官方微博和Twitter",
            "已联系主要媒体进行情况说明",
            "客服团队已准备统一回复模板"
        ],
        "舆情监控部门": [
            "已加强对主要社交平台的监控频率",
            "重点关注大V和KOL的言论动态",
            "舆情热度监控系统已升级预警阈值"
        ],
        "法务部门": [
            "已审查公关声明的法律风险",
            "准备应对可能的用户投诉和索赔",
            "与监管部门保持沟通渠道畅通"
        ]
    }
    
    result = random.choice(results.get(dept_name, ["任务执行完成"]))
    update_department_execution(dept_name, "完成", result)
    print(f"✅ {dept_name} 任务完成: {result}")

def demo_cross_functional_coordinator():
    global sentiment_event
    print("🎯 启动跨部门协调官...")
    update_agent_status("action_coordinator", "执行中", "blue", "正在解析指令并生成行动计划...")
    command = current_state.get('human_command', '无明确指令，自主决策')
    prompt = f"作为行动协调官，根据指令'{command}'和当前态势，生成一个不超过50字的、包含具体部门的简洁行动计划。"
    action_plan = get_llm_response(prompt)
    
    current_state['executed_actions'] = action_plan
    update_system_info("task_distribution", action_plan)
    
    update_agent_status("action_coordinator", "执行中", "blue", "正在模拟执行行动计划...")
    time.sleep(2) # 模拟执行耗时
    update_system_info("department_execution", f"行动计划执行完毕：\n{action_plan}")
    
    update_agent_status("action_coordinator", "完成", "green", "行动计划执行完毕")
    print("⚡️ 行动完成，触发即时舆情更新")
    sentiment_event.set() # 触发舆情更新

def demo_realtime_sentiment():
    print("📊 启动实时舆情监控...")
    update_agent_status("realtime_sentiment_feedback", "执行中", "blue", "正在分析实时舆情...")
    time.sleep(1)
    
    # 根据执行的任务生成相应的舆情反馈
    executed_depts = list(current_state['department_execution'].keys())
    
    if "技术部门" in executed_depts and "公关部门" in executed_depts:
        sentiment_feedback = "网民对技术团队快速响应和官方及时声明表示认可，舆情热度开始下降，但仍有部分用户要求更详细的解释。"
    elif "技术部门" in executed_depts:
        sentiment_feedback = "用户对技术修复行动表示认可，但希望官方能够给出更明确的时间表和补偿方案。"
    elif "公关部门" in executed_depts:
        sentiment_feedback = "官方声明获得部分用户理解，但技术问题尚未解决，用户仍在等待具体的修复进展。"
    else:
        sentiment_feedback = "网民对快速响应表示认可，但仍有部分用户质疑处理效率。舆情热度略有下降。"
    
    current_state['realtime_sentiment'] = sentiment_feedback
    update_system_info("realtime_sentiment", sentiment_feedback)
    update_agent_status("realtime_sentiment_feedback", "完成", "green", "实时舆情分析完成")

def demo_crisis_status_check():
    print("✅ 启动危机状态检查...")
    global waiting_for_crisis_decision
    update_agent_status("status_check", "等待确认", "orange", "等待确认危机状态...")
    
    # 显示当前轮次信息
    cycle_info = f"第 {current_state['cycles']} 轮处理完成，请确认危机状态"
    print(f"📊 {cycle_info}")
    
    socketio.emit('crisis_status_check', {
        'message': f'{cycle_info}\n\n危机是否已经解决？'
    })
    
    waiting_for_crisis_decision = True
    print("⏳ 等待危机状态确认...")
    
    # 添加超时机制，避免无限等待
    timeout_counter = 0
    while waiting_for_crisis_decision and timeout_counter < 300:  # 30秒超时
        time.sleep(0.1)
        timeout_counter += 1
    
    if timeout_counter >= 300:
        print("⚠️ 危机状态确认超时，默认为未解决")
        current_state['crisis_resolved'] = False
        waiting_for_crisis_decision = False
    
    resolved = current_state.get('crisis_resolved', False)
    status_msg = "危机已解决，准备复盘" if resolved else "危机未解决，继续处理"
    update_agent_status("status_check", "完成", "green", status_msg)
    print(f"📋 危机状态确认结果: {status_msg}")
    return resolved

def demo_post_mortem_analyst():
    print("📝 启动复盘分析师...")
    update_agent_status("post_mortem_analyst", "执行中", "blue", "正在生成复盘报告...")
    
    # 生成复盘报告
    report = f"""复盘总结：
- 响应速度：及时，共处理 {current_state['cycles']} 轮危机
- 技术诊断：{current_state.get('technical_report', '无')[:50]}...
- 公关策略：已制定多套方案
- 法律审查：风险评估完成
- 指挥决策：总指挥官指令明确
- 改进建议：建立更完善的预警机制，加强部门间协作"""
    
    current_state['post_mortem_report'] = report
    update_system_info("post_mortem_report", report)
    update_agent_status("post_mortem_analyst", "完成", "green", "复盘报告生成完成")

def reset_agents_for_new_cycle():
    """重置所有智能体状态，准备新一轮处理"""
    # 不需要手动重置，前端会自动处理绿色状态的恢复
    # 只重置那些可能卡在等待状态的智能体
    waiting_agents = ['human_decision_gateway', 'status_check']
    for agent_name in waiting_agents:
        if agent_status[agent_name]['color'] in ['orange', 'blue']:
            update_agent_status(agent_name, "待机", "gray", "")

def run_demo_system():
    """运行演示系统 - 持续循环模式"""
    global system_running
    
    print("🎭 开始运行演示系统...")
    
    try:
        while system_running:
            print(f"\n🔄 ===== 开始第 {current_state['cycles'] + 1} 轮危机处理 =====")
            
            # 1. 舆情哨兵 - 监控危机状态
            demo_sentiment_sentinel()
            if not system_running: break
            
            # 2. 串行分析流程
            demo_technical_diagnostician()
            if not system_running: break
            demo_pr_strategist()
            if not system_running: break
            demo_legal_counsel()
            if not system_running: break
            
            # 3. 决策与执行
            demo_human_decision_gateway()
            if not system_running: break
            demo_cross_functional_coordinator()
            if not system_running: break
            
            # 4. 实时舆情反馈
            demo_realtime_sentiment()
            if not system_running: break

            # 5. 危机状态检查
            resolved = demo_crisis_status_check()
            
            # 6. 学习与记忆
            outcome = "resolved" if resolved else "escalated"
            memory_instance.add_experience(
                crisis=current_state.get('crisis_description', '未知危机')[:50],
                decision=current_state.get('human_command', '未知决策'),
                outcome=outcome
            )
            
            if resolved:
                demo_post_mortem_analyst()
                socketio.emit('system_completed', {'message': '神盾系统执行完成 - 危机已解决'})
                print("✅ 神盾系统执行完成 - 危机已解决")
                break
            else:
                current_state['cycles'] += 1
                current_state['department_execution'] = {}
                print(f"🔄 危机未解决，准备第 {current_state['cycles']} 轮处理...")
                
                # 重置智能体状态
                reset_agents_for_new_cycle()
                time.sleep(2)
        
    except Exception as e:
        print(f"❌ 系统主循环错误: {e}")
        socketio.emit('system_error', {'error': str(e)})
    finally:
        system_running = False
        print("🛑 演示系统主循环已停止")

def continuous_sentiment_monitoring():
    """持续舆情监控线程"""
    global system_running, sentiment_event
    print("📈 持续舆情监控线程已启动")

    while system_running:
        try:
            # 等待30秒或被事件触发
            triggered = sentiment_event.wait(timeout=30)
            if not system_running:
                break
            
            if triggered:
                sentiment_event.clear()
                update_type = "突发"
                print("⚡️ 事件触发，立即更新舆情...")
            else:
                update_type = "周期"
                print("⏰ 周期性更新舆情...")
            
            update_agent_status("realtime_sentiment_feedback", "执行中", "blue", f"正在进行{update_type}舆情分析...")

            # 基于当前所有信息生成舆情报告
            context = f"""
            当前危机: {current_state.get('crisis_description', '无')}
            技术报告: {current_state.get('technical_report', '无')}
            最新行动: {current_state.get('executed_actions', '无')}
            """
            prompt = f"你是一名舆情分析师。根据以下信息，生成一条非常简短（不超过50字）的实时舆情摘要：{context}"
            
            sentiment_summary = get_llm_response(prompt)
            current_state['realtime_sentiment'] = sentiment_summary
            
            update_system_info("realtime_sentiment", sentiment_summary)
            update_agent_status("realtime_sentiment_feedback", "完成", "green", f"{update_type}舆情分析完成")

        except Exception as e:
            print(f"❌ 舆情监控线程错误: {e}")
            time.sleep(5)

    print("📉 持续舆情监控线程已停止")

def update_approval_requests(requests):
    """更新批准请求显示"""
    system_info["approval_requests"] = requests
    socketio.emit('approval_requests_update', {
        'requests': requests
    })

def process_approval_decision(agent_name, approved):
    """处理批准决策"""
    global waiting_for_approval
    
    # 更新当前状态中的批准请求状态
    approval_requests = current_state.get('approval_requests', [])
    for request in approval_requests:
        if request['agent'] == agent_name and request['status'] == 'pending':
            request['status'] = 'approved' if approved else 'rejected'
            request['decision_time'] = time.time()
            break
    
    current_state['approval_requests'] = approval_requests
    current_state['pending_approval'] = any(req['status'] == 'pending' for req in approval_requests)
    
    # 更新显示
    update_approval_requests(approval_requests)
    
    # 只在手动决策时记录日志（避免重复记录）
    decision_text = "批准" if approved else "拒绝"
    print(f"📋 总指挥官{decision_text}了{agent_name}的行动请求")
    
    # 如果没有更多待批准的请求，继续流程
    if not current_state['pending_approval']:
        waiting_for_approval = False
    
    return approved

@app.route('/')
def index():
    return render_template('index.html')

@socketio.on('connect')
def handle_connect():
    print('🔗 客户端已连接')
    emit('connected', {'data': '连接成功 - 演示模式'})

@socketio.on('start_system')
def handle_start_system():
    """启动神盾系统"""
    global system_running, command_processing, sentiment_thread, sentiment_event
    print('🚀 收到启动系统请求')
    if not system_running:
        # 重置系统状态 - 使用正确的初始值
        if LANGGRAPH_AVAILABLE:
            # 使用标准状态初始化
            current_state.update(create_initial_state())
        else:
            # 演示模式的状态重置
            current_state['cycles'] = 0
            current_state['crisis_resolved'] = False
            current_state['crisis_description'] = None
            current_state['technical_report'] = None
            current_state['pr_strategy'] = None
            current_state['legal_review'] = None
            current_state['human_command'] = None
            current_state['parsed_tasks'] = None
            current_state['executed_actions'] = None
            current_state['incident_log'] = []
            current_state['department_execution'] = {}
            current_state['approval_requests'] = []
            current_state['pending_approval'] = False
            current_state['action_log'] = []
        
        system_running = True
        sentiment_event.clear()

        # 启动后台线程
        command_thread = threading.Thread(target=command_processor, daemon=True)
        command_thread.start()
        
        sentiment_thread = threading.Thread(target=continuous_sentiment_monitoring, daemon=True)
        sentiment_thread.start()
        
        print('🎯 指令处理器和舆情监控线程已启动')
        
        # 根据LangGraph可用性选择运行模式
        if LANGGRAPH_AVAILABLE:
            print("🛡️ 启动真实神盾系统（LangGraph + DeepSeek API）")
            thread = threading.Thread(target=run_real_aegis_system)
            emit('system_started', {'message': '神盾系统已启动 - 真实模式（DeepSeek API）\n✅ 总指挥官可随时下达命令'})
        else:
            print("🎭 启动演示模式")
            thread = threading.Thread(target=run_demo_system)
            emit('system_started', {'message': '神盾系统已启动 - 演示模式\n✅ 总指挥官可随时下达命令'})
        
        thread.daemon = True
        thread.start()
        print('✅ 神盾系统启动线程已创建')
    else:
        emit('system_started', {'message': '系统已在运行中\n✅ 总指挥官可随时下达命令'})

@socketio.on('human_command')
def handle_human_command(data):
    """处理人类指令 - 智能路由到合适的处理方式"""
    global waiting_for_human_input
    command = data.get('command', '')
    print(f'📨 收到总指挥官指令: {command}')
    
    # 如果系统正在等待人类输入（传统流程），直接设置并继续流程
    if waiting_for_human_input:
        current_state['human_command'] = command
        waiting_for_human_input = False
        emit('command_received', {'message': f'指令已接收（流程中）: {command}'})
        print("✅ 指令已传递给主流程，继续执行...")
    else:
        # 否则加入指令队列立即处理（不中断主流程）
        command_queue.put({'command': command, 'type': 'immediate'})
        emit('command_received', {'message': f'指令已接收（立即处理）: {command}'})
        print("✅ 指令已加入队列，将立即处理...")

@socketio.on('crisis_decision')
def handle_crisis_decision(data):
    """处理危机状态决策"""
    global waiting_for_crisis_decision
    resolved = data.get('resolved', False)
    print(f'📋 收到危机状态决策: {"已解决" if resolved else "未解决"}')
    current_state['crisis_resolved'] = resolved
    waiting_for_crisis_decision = False
    emit('crisis_decision_received', {'resolved': resolved})

@socketio.on('approval_decision')
def handle_approval_decision(data):
    """处理批准决策"""
    agent_name = data.get('agent_name', '')
    approved = data.get('approved', False)
    
    print(f'📋 收到批准决策: {agent_name} - {"批准" if approved else "拒绝"}')
    
    result = process_approval_decision(agent_name, approved)
    
    # 记录人类指挥官的决策到日志
    decision_text = "批准" if result else "拒绝"
    new_log_entry = f"总指挥官{decision_text}了{agent_name}的行动请求"
    current_state['incident_log'] = current_state.get('incident_log', []) + [new_log_entry]
    
    # 更新事件日志显示
    update_system_info('incident_log', current_state['incident_log'])
    
    emit('approval_decision_received', {
        'agent_name': agent_name,
        'approved': result,
        'message': f'{"批准" if result else "拒绝"}了{agent_name}的行动请求'
    })

@socketio.on('stop_system')
def handle_stop_system():
    """停止神盾系统"""
    global system_running, command_processing, sentiment_event, waiting_for_approval
    print('🛑 收到停止系统请求')
    system_running = False
    command_processing = False
    waiting_for_approval = False
    sentiment_event.set()  # 唤醒并终止舆情线程
    
    # 重置所有智能体状态到待机
    for agent_name in agent_status.keys():
        update_agent_status(agent_name, "待机", "gray", "")
    
    # 清空批准请求
    current_state['approval_requests'] = []
    current_state['pending_approval'] = False
    update_approval_requests([])
    
    emit('system_stopped', {'message': '神盾系统已停止'})

# 重写智能体模块中的批准等待函数，使其适配Web环境
def web_wait_for_approval_or_timeout(agent_name: str, timeout_seconds: int = 30) -> bool:
    """
    (v2.0) Web环境下的批准等待函数 - 修复了竞态条件问题。
    """
    global waiting_for_approval
    print(f"⏳ {agent_name} 等待总指挥官批准... (超时时间: {timeout_seconds}秒)")
    
    waiting_for_approval = True
    start_time = time.time()
    
    # 循环等待，直到超时或收到决策
    while time.time() - start_time < timeout_seconds and waiting_for_approval:
        # 在这个循环中，我们只等待 waiting_for_approval 变量被外部线程（SocketIO）改变。
        # 不再在循环内部检查状态，避免逻辑混乱。
        time.sleep(0.2)
    
    # --- 循环结束，现在判断结束原因 ---

    # 1. 检查是否是超时导致的结束
    if waiting_for_approval:  # 如果仍然为 True，说明是超时
        print(f"⚠️ {agent_name} 批准等待超时，AI数字分身自动批准")
        waiting_for_approval = False
        
        # 让AI数字分身处理批准
        process_approval_decision(agent_name, True)
        
        # 记录AI数字分身决策到日志（只有超时情况才在这里记录）
        new_log_entry = f"AI数字分身自动批准了{agent_name}的行动请求（超时）"
        current_state['incident_log'] = current_state.get('incident_log', []) + [new_log_entry]
        update_system_info('incident_log', current_state['incident_log'])
        
        return True  # 超时后自动批准

    # 2. 如果不是超时，说明总指挥官已决策，我们需要查找决策结果
    else:
        print(f"🔍 {agent_name} 已收到决策，正在确认结果...")
        final_decision = False  # 默认为拒绝
        
        # 再次从全局状态中查找最终的决策结果
        approval_requests = current_state.get('approval_requests', [])
        for request in approval_requests:
            if request['agent'] == agent_name:
                if request['status'] == 'approved':
                    print(f"✅ {agent_name} 确认获得总指挥官批准")
                    final_decision = True
                    break
                elif request['status'] == 'rejected':
                    print(f"❌ {agent_name} 确认被总指挥官拒绝")
                    final_decision = False
                    break
        
        return final_decision

def web_request_approval_from_commander(agent_name: str, proposed_action: str, current_situation: str) -> dict:
    """
    Web环境下的批准请求函数
    """
    print(f"--- {agent_name}：请求行动批准 ---")
    
    # 生成批准请求说明
    from aegis_system.agents import approval_request_prompt, get_llm_response
    
    prompt = approval_request_prompt.format(
        agent_name=agent_name,
        proposed_action=proposed_action,
        current_situation=current_situation
    )
    
    request_explanation = get_llm_response(prompt)
    print(f"📋 {agent_name} 请求批准: {request_explanation}")
    
    # 记录批准请求
    approval_request = {
        "agent": agent_name,
        "action": proposed_action,
        "explanation": request_explanation,
        "status": "pending",
        "timestamp": time.time()
    }
    
    # 更新状态中的批准请求
    approval_requests = current_state.get('approval_requests', [])
    approval_requests.append(approval_request)
    current_state['approval_requests'] = approval_requests
    current_state['pending_approval'] = True
    
    # 更新显示
    update_approval_requests(approval_requests)
    
    # 发送批准请求通知
    socketio.emit('approval_request', {
        'agent': agent_name,
        'action': proposed_action,
        'explanation': request_explanation
    })
    
    new_log_entry = f"{agent_name} 请求行动批准: {request_explanation}"
    current_state['incident_log'] = current_state.get('incident_log', []) + [new_log_entry]
    
    return {
        "approval_requests": approval_requests,
        "incident_log": current_state['incident_log'],
        "pending_approval": True
    }

# 在导入智能体模块后，替换批准相关函数
if LANGGRAPH_AVAILABLE:
    try:
        import aegis_system.agents as agents_module
        # 替换批准相关函数
        agents_module.wait_for_approval_or_timeout = web_wait_for_approval_or_timeout
        agents_module.request_approval_from_commander = lambda agent_name, proposed_action, current_situation, state: web_request_approval_from_commander(agent_name, proposed_action, current_situation)
        print("✅ 已替换智能体模块中的批准函数为Web版本")
    except ImportError:
        print("⚠️ 无法导入智能体模块，批准功能可能无法正常工作")

if __name__ == '__main__':
    print("🛡️ 神盾危机管理系统 - 中控大屏启动中...")
    print("📡 访问地址: http://localhost:5000")
    
    if LANGGRAPH_AVAILABLE:
        print("🔥 真实模式：将使用LangGraph + DeepSeek API")
        # 检查API密钥
        api_key = os.getenv('DEEPSEEK_API_KEY')
        if api_key:
            print("✅ DeepSeek API密钥已配置")
        else:
            print("⚠️ 未找到DeepSeek API密钥，请检查.env文件")
    else:
        print("🎭 演示模式：使用模拟数据")
    
    socketio.run(app, debug=False, host='0.0.0.0', port=5000) 