import os
import random
import json
import asyncio
import concurrent.futures
import openai
import time
from langchain_openai import ChatOpenAI
from langchain.prompts import PromptTemplate
from langchain.schema import SystemMessage, HumanMessage

from aegis_system.state import AegisState
import config

# --- LLM Client Setup ---

# The client is now initialized in the web_app and passed to the graph runner
# For direct testing, you might need to load .env here.
# from dotenv import load_dotenv
# load_dotenv()

llm_client = openai.OpenAI(
    api_key=os.getenv("DEEPSEEK_API_KEY"),
    base_url=os.getenv("DEEPSEEK_BASE_URL")
)

def get_llm_response(prompt: str) -> str:
    """A unified function to call the LLM and get a response."""
    try:
        response = llm_client.chat.completions.create(
            model=os.getenv("DEEPSEEK_MODEL", "deepseek-chat"),
            messages=[{"role": "user", "content": prompt}],
            max_tokens=800,
            temperature=0.7,
        )
        content = response.choices[0].message.content.strip()
        return content
    except Exception as e:
        print(f"❌ LLM call failed: {e}")
        return f"LLM响应错误: {str(e)}"

# --- 大语言模型和提示词初始化 ---

# 使用提供的DeepSeek配置初始化大语言模型
llm = ChatOpenAI(
    api_key=config.deepseek_api_key,
    base_url=config.deepseek_base_url,
    model=config.deepseek_model,
    temperature=0.7,
)

# 修复：技术诊断官的提示词模板 - 专注于技术分析
tech_prompt = PromptTemplate.from_template(
    """
你是一个大型全球社交媒体平台的首席技术诊断官。
请对技术故障进行详细的根因分析和解决方案制定。

用户报告的技术症状：
"{crisis_description}"

请详细回答以下问题：
1. 技术问题的可能根因分析（至少3个可能原因）
2. 问题的严重程度评估和影响范围
3. 详细的解决方案和实施步骤
4. 预估修复时间和所需资源
5. 预防类似问题的长期措施

请提供专业、详细的技术诊断报告：
"""
)

# 新：用于总结技术报告的提示词
tech_summary_prompt = PromptTemplate.from_template(
    """
将以下详细的技术诊断报告压缩成一个核心技术问题描述，保留关键信息。

详细报告：
"{technical_report}"

核心技术问题摘要：
"""
)

# 修复：公关策略师的提示词模板 - 使用模板库
pr_prompt = PromptTemplate.from_template(
    """
你是资深公关策略师。基于技术问题摘要和提供的模板库，为每种沟通策略制定详细的公关方案。

技术问题摘要：{technical_summary}

可用模板库：
{pr_templates}

请为每种策略提供详细的公关方案。你必须严格按照以下JSON格式输出，不要添加任何其他文字：

{{
  "sincere": {{
    "message": "基于坦诚模板定制的详细声明内容",
    "strategy": "具体实施策略和要点说明"
  }},
  "reassuring": {{
    "message": "基于安抚模板定制的详细声明内容", 
    "strategy": "具体实施策略和要点说明"
  }},
  "deflecting": {{
    "message": "基于拖延模板定制的详细声明内容",
    "strategy": "具体实施策略和要点说明"
  }}
}}

重要提醒：
1. 只输出JSON，不要有任何前缀或后缀文字
2. 确保JSON格式正确，所有字符串都用双引号
3. message字段应包含完整的公关声明
4. strategy字段应说明该策略的核心要点和实施建议
"""
)

# 法务顾问的提示词模板
legal_prompt = PromptTemplate.from_template(
    """
你是法务与合规顾问。
请对以下公关策略进行全面的法律风险评估，特别关注：
- 承认过错的法律后果
- 数据泄露相关的合规风险
- 监管机构（如GDPR/CCPA）的合规要求
- 潜在的诉讼风险
- 股东和投资者关系影响

待审查的公关策略：
{pr_strategy}

请为每个策略提供详细的法律分析，包括：
1. 法律风险等级（低/中/高）
2. 具体风险点分析
3. 合规建议和修改意见
4. 推荐的法律保护措施

你的详细法律审查报告：
"""
)

# 复盘分析师的提示词模板
post_mortem_prompt = PromptTemplate.from_template(
    """
你是复盘分析师。
危机已经解决。请基于完整的事件日志编写一份详细的内部复盘报告。

完整事件日志：
{incident_log}

请提供包含以下内容的详细复盘报告：
1. 事件时间线和关键节点分析
2. 决策过程评估和效果分析
3. 各部门协作情况评价
4. 响应速度和质量评估
5. 识别的优势和成功经验
6. 发现的问题和改进机会
7. 具体的改进建议和行动计划
8. 预防类似事件的长期措施

你的详细复盘报告：
"""
)

# 新：用于危机升级的提示词
sentiment_escalation_prompt = PromptTemplate.from_template(
    """
你是一个危机模拟器。当前危机未能解决，现在需要升级。
请基于上一轮的【事件摘要】，生成一个新的、更严重的【升级后舆情】。
可以引入新的负面因素，例如：股价下跌、关键意见领袖（KOL）发声、竞争对手行动、监管机构关注等。

【事件摘要】：
{event_summary}

【升级后舆情】：
"""
)

# 新：实时舆情反馈提示词
realtime_sentiment_prompt = PromptTemplate.from_template(
    """
你是舆情哨兵，负责实时监控网络舆情变化。
刚才各部门执行了以下行动，请分析这些行动对当前舆情的详细影响。

【当前危机背景】：
{current_crisis}

【刚才执行的行动】：
{recent_actions}

请生成一个详细的舆情变化报告，包括：
1. 网民对这些行动的即时反应和情绪变化
2. 主要社交平台的舆情热度变化趋势
3. 关键意见领袖(KOL)和媒体的反应
4. 是否出现新的关注点或衍生话题
5. 舆情风险评估和预警
6. 下一步舆情监控重点

详细舆情分析报告：
"""
)

# 新：指挥官指令解析提示词
command_parser_prompt = PromptTemplate.from_template(
    """
你是指挥中心的高级指令解析系统。总指挥官刚刚下达了一个复合指令，你需要将其解析为具体的任务分配。

可分配的部门：
- 技术部门 (tech_dept): 负责技术问题诊断、系统修复、架构优化
- 公关部门 (pr_dept): 负责对外沟通、媒体关系、声誉管理
- 舆情监控 (sentiment_monitor): 负责实时监控、数据分析、趋势预测
- 法务部门 (legal_dept): 负责合规审查、风险评估、法律保护

总指挥官指令：
"{command}"

请仔细分析指令内容，将其分解为具体的、可执行的任务。每个任务应该：
1. 明确指定负责部门
2. 详细描述具体行动要求
3. 包含预期成果和时间要求

请按以下JSON格式输出任务分配：
{{
  "tasks": [
    {{"dept": "部门代码", "action": "详细的任务描述和执行要求"}},
    {{"dept": "部门代码", "action": "详细的任务描述和执行要求"}}
  ]
}}

任务分配结果：
"""
)

# 新：各部门执行任务的提示词
dept_execution_prompts = {
    "tech_dept": PromptTemplate.from_template(
        """
你是技术部门负责人。总指挥官通过协调中心给你下达了紧急任务。

当前技术状况：{status}
分配给你的任务：{task}

请立即执行并提供详细的执行报告，包括：
1. 任务理解和执行计划
2. 具体实施步骤和进展
3. 遇到的技术挑战和解决方案
4. 当前执行状态和完成度
5. 预期效果和风险评估
6. 需要其他部门配合的事项

详细执行报告：
"""
    ),
    "pr_dept": PromptTemplate.from_template(
        """
你是公关部门负责人。总指挥官通过协调中心给你下达了重要任务。

当前公关状况：{status}
分配给你的任务：{task}

请立即执行并提供详细的执行报告，包括：
1. 任务分析和沟通策略
2. 目标受众和传播渠道选择
3. 核心信息制定和表达方式
4. 执行时机和节奏控制
5. 预期传播效果和风险控制
6. 后续跟进和调整计划

详细执行报告：
"""
    ),
    "sentiment_monitor": PromptTemplate.from_template(
        """
你是舆情监控部门负责人。总指挥官通过协调中心给你下达了监控任务。

当前舆情状况：{status}
分配给你的任务：{task}

请立即执行并提供详细的监控报告，包括：
1. 监控范围和重点平台确定
2. 关键指标和数据收集方法
3. 实时监控结果和趋势分析
4. 异常情况识别和预警机制
5. 数据解读和影响评估
6. 监控建议和优化方案

详细监控报告：
"""
    ),
    "legal_dept": PromptTemplate.from_template(
        """
你是法务部门负责人。总指挥官通过协调中心给你下达了法律任务。

当前法务状况：{status}
分配给你的任务：{task}

请立即执行并提供详细的法务报告，包括：
1. 法律问题识别和风险评估
2. 相关法规和合规要求分析
3. 法律保护措施和应对策略
4. 潜在法律后果和影响评估
5. 合规建议和操作指引
6. 法律文件准备和程序安排

详细法务报告：
"""
    )
}

# 添加批准请求相关的提示词
approval_request_prompt = """
你是{agent_name}，准备执行以下行动：

【拟执行行动】：
{proposed_action}

【当前态势】：
{current_situation}

请简洁地向总指挥官说明你的行动计划和理由（50字以内）：
"""

# --- 智能体函数 ---

def sentiment_sentinel(state: AegisState) -> dict:
    """
    模拟监控社交媒体并触发/升级危机。
    这是图的循环入口点。
    """
    print("--- 智能体：舆情哨兵 ---")

    current_cycle = state.get('cycles', 0)
    event_summary = state.get('crisis_description', '无')

    if current_cycle == 0:
        # 第一次循环：从一组初始事件中随机选择一个
        possible_events = [
            "大量用户报告登录失败，#平台宕机 在Twitter上热搜。用户情绪极度负面且呈指数级增长。",
            "用户反馈应用内支付功能无法使用，多个头部KOL抱怨无法收到打赏，#支付失灵 话题开始发酵。",
            "核心数据库出现意料之外的主从切换，导致全站范围内的内容加载延迟和数据不一致问题。",
            "一个主要CDN节点遭受DDoS攻击，导致亚太地区用户访问速度急剧下降，社交媒体上怨声载道。"
        ]
        crisis = random.choice(possible_events)
        print(f"🔥 新的危机事件：{crisis}")
        return {
            "crisis_description": crisis,
            "incident_log": [f"周期 {current_cycle}: 舆情哨兵监测到新的危机事件 - {crisis}"]
        }
    else:
        # 后续循环：如果危机未解决，则升级危机
        print(f"🔥 危机升级（第 {current_cycle} 轮）")
        prompt = sentiment_escalation_prompt.format(event_summary=event_summary)
        escalated_crisis = get_llm_response(prompt)
        print(f"🔥 升级后的危机：{escalated_crisis}")
        
        new_log_entry = f"周期 {current_cycle}: 危机升级 - {escalated_crisis}"
        updated_log = state.get('incident_log', []) + [new_log_entry]

        return {
            "crisis_description": escalated_crisis,
            "incident_log": updated_log
        }


def chief_technical_diagnostician(state: AegisState) -> dict:
    """
    技术诊断官：专注于技术层面的问题分析和解决方案。
    """
    print("--- 智能体：首席技术诊断官 ---")
    
    # 获取当前危机描述
    crisis_description = state.get('crisis_description', '无危机信息')
    
    # 准备执行的行动
    proposed_action = "对当前技术故障进行深度诊断分析，识别根本原因并提出技术解决方案"
    current_situation = f"危机状况: {crisis_description}"
    
    # 请求批准
    approval_result = request_approval_from_commander(
        agent_name="technical_diagnostician",
        proposed_action=proposed_action,
        current_situation=current_situation,
        state=state
    )
    
    # 更新状态以包含批准请求
    state.update(approval_result)
    
    # 等待批准
    approved = wait_for_approval_or_timeout("technical_diagnostician", timeout_seconds=30)
    
    if not approved:
        print("❌ 首席技术诊断官：未获得批准，停止执行")
        new_log_entry = "首席技术诊断官：未获得执行批准"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {
            "incident_log": updated_log,
            "pending_approval": False
        }
    
    print("✅ 首席技术诊断官：获得批准，开始执行技术诊断")
    
    # API保护延迟
    add_api_protection_delay()
    
    # 执行技术诊断
    chain = tech_prompt | llm
    response = chain.invoke({"crisis_description": crisis_description})
    technical_report = response.content
    
    print(f"🔧 技术诊断报告: {technical_report}")
    
    new_log_entry = f"首席技术诊断官完成技术分析: {technical_report[:50]}..."
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {
        "technical_report": technical_report,
        "incident_log": updated_log,
        "pending_approval": False
    }


def pr_strategist(state: AegisState) -> dict:
    """
    公关策略师：制定危机公关策略和对外沟通方案。
    """
    print("--- 智能体：公关策略师 ---")
    
    # 获取当前信息
    crisis_description = state.get('crisis_description', '无危机信息')
    technical_report = state.get('technical_report', '无技术报告')
    
    # 准备执行的行动
    proposed_action = "制定危机公关策略，包括对外声明、媒体应对和用户沟通方案"
    current_situation = f"危机状况: {crisis_description}\n技术分析: {technical_report}"
    
    # 请求批准
    approval_result = request_approval_from_commander(
        agent_name="pr_strategist",
        proposed_action=proposed_action,
        current_situation=current_situation,
        state=state
    )
    
    # 更新状态以包含批准请求
    state.update(approval_result)
    
    # 等待批准
    approved = wait_for_approval_or_timeout("pr_strategist", timeout_seconds=30)
    
    if not approved:
        print("❌ 公关策略师：未获得批准，停止执行")
        new_log_entry = "公关策略师：未获得执行批准"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {
            "incident_log": updated_log,
            "pending_approval": False
        }
    
    print("✅ 公关策略师：获得批准，开始制定公关策略")
    
    # API保护延迟
    add_api_protection_delay()
    
    # 执行公关策略制定
    chain = pr_prompt | llm
    response = chain.invoke({
        "crisis_description": crisis_description,
        "technical_report": technical_report
    })
    
    raw_response = response.content
    print(f"📢 公关策略师原始输出: {raw_response}")
    
    # 解析JSON格式的公关策略
    try:
        pr_strategy = json.loads(raw_response)
        print(f"✅ 公关策略解析成功: {pr_strategy}")
    except json.JSONDecodeError:
        print("⚠️ JSON解析失败，使用保底策略")
        pr_strategy = {
            "immediate_statement": "我们已经注意到用户反馈的问题，技术团队正在紧急处理中。",
            "media_response": "积极配合媒体询问，提供透明的问题说明。",
            "user_communication": "通过官方渠道及时更新处理进展，安抚用户情绪。",
            "timeline": "立即发布声明，每小时更新进展。"
        }
    
    new_log_entry = f"公关策略师完成策略制定: {pr_strategy.get('immediate_statement', '策略已制定')[:50]}..."
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {
        "pr_strategy": pr_strategy,
        "incident_log": updated_log,
        "pending_approval": False
    }


def legal_counsel(state: AegisState) -> dict:
    """
    法务与合规顾问：评估法律风险并提供合规建议。
    """
    print("--- 智能体：法务与合规顾问 ---")
    
    # 获取当前信息
    crisis_description = state.get('crisis_description', '无危机信息')
    pr_strategy = state.get('pr_strategy', {})
    
    # 准备执行的行动
    proposed_action = "评估当前危机的法律风险，审查公关策略的合规性，提供法律建议"
    current_situation = f"危机状况: {crisis_description}\n公关策略: {json.dumps(pr_strategy, ensure_ascii=False)}"
    
    # 请求批准
    approval_result = request_approval_from_commander(
        agent_name="legal_counsel",
        proposed_action=proposed_action,
        current_situation=current_situation,
        state=state
    )
    
    # 更新状态以包含批准请求
    state.update(approval_result)
    
    # 等待批准
    approved = wait_for_approval_or_timeout("legal_counsel", timeout_seconds=30)
    
    if not approved:
        print("❌ 法务与合规顾问：未获得批准，停止执行")
        new_log_entry = "法务与合规顾问：未获得执行批准"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {
            "incident_log": updated_log,
            "pending_approval": False
        }
    
    print("✅ 法务与合规顾问：获得批准，开始法律风险评估")
    
    # API保护延迟
    add_api_protection_delay()
    
    # 执行法律风险评估
    chain = legal_prompt | llm
    response = chain.invoke({
        "crisis_description": crisis_description,
        "pr_strategy": json.dumps(pr_strategy, ensure_ascii=False)
    })
    
    legal_review = response.content
    print(f"⚖️ 法律审查结果: {legal_review}")
    
    new_log_entry = f"法务与合规顾问完成风险评估: {legal_review[:50]}..."
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {
        "legal_review": legal_review,
        "incident_log": updated_log,
        "pending_approval": False
    }


def human_decision_gateway(state: AegisState) -> dict:
    """
    解析人类指挥官的指令。
    """
    print("--- 智能体：总指挥官决策中心 ---")
    command = state.get('human_command')
    if not command:
        # 这是AI数字分身决策的入口
        print("🤔 未收到人类指令，AI数字分身将接管。")
        # 在真实应用中，这里会调用AI分身逻辑
        # 为保持流程，我们假定AI决定执行一个标准操作
        parsed_tasks = {
            "tasks": [{
                "dept": "pr_dept",
                "action": "发布一个安抚性的声明，并承诺会尽快提供更新。"
            }]
        }
    else:
        # 解析人类指令
        parser_chain = command_parser_prompt | llm
        response = parser_chain.invoke({"command": command})
        raw_response = response.content
        print(f"📝 来自指令解析器的原始响应: {raw_response}")
        try:
            parsed_tasks = json.loads(raw_response)
        except json.JSONDecodeError:
            print("❌ 指令解析失败，不是有效的JSON。")
            parsed_tasks = {"tasks": []} # 返回一个空任务列表

    print(f"✅ 解析后的任务: {parsed_tasks}")

    new_log_entry = f"总指挥官决策中心解析指令，分配了 {len(parsed_tasks.get('tasks', []))} 个任务。"
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {"parsed_tasks": parsed_tasks, "incident_log": updated_log}

async def execute_department_task(dept: str, action: str, state: AegisState):
    """
    异步执行单个部门的任务。
    """
    # 动态获取各部门当前状态的辅助函数
    def get_status(department):
        status_map = {
            "tech_dept": state.get('technical_report', 'N/A'),
            "pr_dept": str(state.get('pr_strategy', 'N/A')),
            "sentiment_monitor": state.get('crisis_description', 'N/A'),
            "legal_dept": state.get('legal_review', 'N/A'),
        }
        return status_map.get(department, '未知状态')

    prompt_template = dept_execution_prompts.get(dept)
    if not prompt_template:
        return f"错误：找不到 {dept} 的执行模板。"

    prompt = prompt_template.format(task=action, status=get_status(dept))
    
    # 注意：这里的get_llm_response是同步的。在真实的async应用中，这里会用异步的LLM客户端。
    # 为了在当前架构下工作，我们在线程池中运行它。
    loop = asyncio.get_running_loop()
    with concurrent.futures.ThreadPoolExecutor() as pool:
        result = await loop.run_in_executor(
            pool, 
            get_llm_response, 
            prompt
        )
    return result

def realtime_sentiment_feedback(state: AegisState) -> dict:
    """
    在行动后提供实时的舆情反馈。
    """
    print("--- 智能体：实时舆情反馈 ---")
    
    current_crisis = state.get('crisis_description', '无背景信息')
    recent_actions = state.get('executed_actions', '无行动执行')
    
    prompt = realtime_sentiment_prompt.format(
        current_crisis=current_crisis,
        recent_actions=json.dumps(recent_actions, ensure_ascii=False)
    )
    feedback = get_llm_response(prompt)
    print(f"💬 实时舆情反馈: {feedback}")

    new_log_entry = f"实时舆情反馈: {feedback}"
    updated_log = state.get('incident_log', []) + [new_log_entry]

    return {"realtime_sentiment": feedback, "incident_log": updated_log}

def cross_functional_action_coordinator(state: AegisState) -> dict:
    """
    跨部门行动协调官：并发执行所有已解析的任务。
    """
    print("--- 智能体：跨部门行动协调官 ---")
    parsed_tasks = state.get('parsed_tasks', {}).get('tasks', [])
    
    if not parsed_tasks:
        print("🟡 没有任务需要执行。")
        return {"executed_actions": {}}

    # 准备执行的行动
    task_summary = "; ".join([f"{task['dept']}: {task['action']}" for task in parsed_tasks])
    proposed_action = f"协调执行以下部门任务: {task_summary}"
    current_situation = f"待执行任务数量: {len(parsed_tasks)}\n任务详情: {task_summary}"
    
    # 请求批准
    approval_result = request_approval_from_commander(
        agent_name="action_coordinator",
        proposed_action=proposed_action,
        current_situation=current_situation,
        state=state
    )
    
    # 更新状态以包含批准请求
    state.update(approval_result)
    
    # 等待批准
    approved = wait_for_approval_or_timeout("action_coordinator", timeout_seconds=30)
    
    if not approved:
        print("❌ 跨部门行动协调官：未获得批准，停止执行")
        new_log_entry = "跨部门行动协调官：未获得执行批准"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {
            "incident_log": updated_log,
            "pending_approval": False
        }
    
    print("✅ 跨部门行动协调官：获得批准，开始协调执行任务")
    print(f"🚀 开始并发执行 {len(parsed_tasks)} 个任务...")
    
    # API保护延迟
    add_api_protection_delay()
    
    # 定义状态映射函数
    def get_dept_status(dept_name):
        status_mapping = {
            'tech_dept': state.get('technical_report', '无技术报告'),
            'pr_dept': str(state.get('pr_strategy', '无公关策略')),
            'sentiment_monitor': state.get('crisis_description', '无危机描述'),
            'legal_dept': state.get('legal_review', '无法律审查')
        }
        return status_mapping.get(dept_name, '未知状态')
    
    results = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(parsed_tasks)) as executor:
        # 创建一个future到部门的映射
        future_to_dept = {}
        
        for task in parsed_tasks:
            dept = task['dept']
            action = task['action']
            
            if dept in dept_execution_prompts:
                prompt = dept_execution_prompts[dept].format(
                    task=action,
                    status=get_dept_status(dept)
                )
                future = executor.submit(get_llm_response, prompt)
                future_to_dept[future] = dept
            else:
                print(f"⚠️ 未找到部门 {dept} 的执行模板")

        for future in concurrent.futures.as_completed(future_to_dept):
            dept = future_to_dept[future]
            try:
                result = future.result()
                results[dept] = result
                print(f"✅ {dept} 执行完成: {result}")
            except Exception as exc:
                error_msg = f"❌ {dept} 执行时发生错误: {exc}"
                print(error_msg)
                results[dept] = error_msg

    print("🏁 所有任务执行完毕。")
    
    new_log_entry = f"跨部门协调官执行了以下任务: {json.dumps(results, ensure_ascii=False)}"
    updated_log = state.get('incident_log', []) + [new_log_entry]

    return {
        "executed_actions": results,
        "incident_log": updated_log,
        "pending_approval": False
    }


def crisis_status_check(state: AegisState) -> dict:
    """
    检查危机状态并更新相应的状态字段。
    """
    print("--- 智能体：危机状态检查 ---")
    human_command = state.get('human_command', '').lower()

    if "危机已解除" in human_command or "结束" in human_command:
        print("✅ 指挥官确认危机已解除。")
        new_log_entry = "危机状态检查：指挥官确认危机已解除"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {"crisis_resolved": True, "incident_log": updated_log}
    else:
        print("🔄 危机尚未解决或有新指令，准备进入下一轮。")
        new_log_entry = f"危机状态检查：危机尚未解决，准备第 {state.get('cycles', 0) + 1} 轮处理"
        updated_log = state.get('incident_log', []) + [new_log_entry]
        return {"crisis_resolved": False, "incident_log": updated_log}

def post_mortem_analyst(state: AegisState) -> dict:
    """
    在危机结束后编写复盘报告。
    """
    print("--- 智能体：复盘分析师 ---")
    
    incident_log = state.get('incident_log', [])
    log_str = "\n".join(incident_log)
    
    chain = post_mortem_prompt | llm
    response = chain.invoke({"incident_log": log_str})
    report = response.content
    
    print(f"📝 复盘报告:\n{report}")
    
    return {"post_mortem_report": report}

def realtime_sentiment_display(state: AegisState) -> dict:
    """
    这是一个虚拟节点，仅用于在UI上显示实时反馈，然后无缝过渡到下一个状态。
    它必须返回至少一个状态字段以满足LangGraph要求。
    """
    print("--- 节点：显示实时舆情 ---")
    # 返回当前的实时舆情状态，确保LangGraph要求得到满足
    current_sentiment = state.get('realtime_sentiment', '舆情监控正常')
    
    # 记录日志条目
    new_log_entry = "实时舆情显示节点：UI更新完成"
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {
        "realtime_sentiment": current_sentiment,
        "incident_log": updated_log
    }

def ai_digital_twin_decision_maker(state: AegisState) -> dict:
    """
    AI数字分身，在人类没有下达指令时自动决策。
    """
    print("--- 智能体：AI数字分身决策 ---")

    # 构建决策所需上下文
    # 修复：将 .join 操作移出 f-string 以避免 SyntaxError
    log_str = "\n".join(state.get('incident_log', []))
    context = f"""
    危机描述: {state.get('crisis_description', 'N/A')}
    技术报告: {state.get('technical_report', 'N/A')}
    公关策略: {json.dumps(state.get('pr_strategy', 'N/A'), ensure_ascii=False)}
    法律审查: {state.get('legal_review', 'N/A')}
    事件日志: {log_str}
    """

    prompt = f"""
你是一个AI数字分身，你的唯一目标是维护公司最大利益。
在人类指挥官缺席的情况下，你需要根据以下当前态势自主决策。
决策应该是具体的、可执行的，并且能推动危机解决。

当前态势:
{context}

基于以上信息，下达一个简洁明确的指令，分配给最合适的部门（技术、公关、法务）。
你的指令（50字以内）：
"""
    
    decision = get_llm_response(prompt)
    print(f"🤖 AI数字分身决策: {decision}")
    
    # 将AI决策格式化为与人类指令相同的格式
    # 这里我们让另一个LLM将自然语言指令转换为结构化任务
    parser_chain = command_parser_prompt | llm
    response = parser_chain.invoke({"command": decision})
    raw_response = response.content
    try:
        parsed_tasks = json.loads(raw_response)
    except json.JSONDecodeError:
        print("❌ AI决策解析失败，不是有效的JSON。")
        # 提供一个备用任务，防止流程中断
        parsed_tasks = {"tasks": [{"dept": "pr_dept", "action": "发布安抚性声明，承诺调查问题。"}]}
        
    print(f"✅ AI决策解析后的任务: {parsed_tasks}")

    new_log_entry = f"AI数字分身介入决策，分配任务: {json.dumps(parsed_tasks, ensure_ascii=False)}"
    updated_log = state.get('incident_log', []) + [new_log_entry]

    return {"parsed_tasks": parsed_tasks, "incident_log": updated_log}

def request_approval_from_commander(agent_name: str, proposed_action: str, current_situation: str, state: AegisState) -> dict:
    """
    向总指挥官请求行动批准
    """
    print(f"--- {agent_name}：请求行动批准 ---")
    
    # 生成批准请求说明
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
    approval_requests = state.get('approval_requests', [])
    approval_requests.append(approval_request)
    
    new_log_entry = f"{agent_name} 请求行动批准: {request_explanation}"
    updated_log = state.get('incident_log', []) + [new_log_entry]
    
    return {
        "approval_requests": approval_requests,
        "incident_log": updated_log,
        "pending_approval": True
    }

def wait_for_approval_or_timeout(agent_name: str, timeout_seconds: int = 30) -> bool:
    """
    等待批准或超时，返回是否获得批准
    """
    print(f"⏳ {agent_name} 等待批准中... (超时时间: {timeout_seconds}秒)")
    
    start_time = time.time()
    while time.time() - start_time < timeout_seconds:
        # 检查全局状态中的批准状态
        # 这个函数会在Web应用环境中被重写，这里提供基本实现
        time.sleep(1)
        
        # 在实际的Web应用中，这里会检查current_state中的approval_requests
        # 查找对应agent_name的批准状态
        
        # 模拟批准检查逻辑（在Web应用中会被实际的批准机制替代）
        # 暂时返回True以保持流程继续，实际实现会在Web应用中处理
        
    print(f"⚠️ {agent_name} 批准等待超时，AI数字分身自动批准")
    return True

# 在Web应用环境中，这个函数会被重写
def check_approval_status(agent_name: str) -> str:
    """
    检查特定智能体的批准状态
    返回: 'pending', 'approved', 'rejected'
    """
    # 这个函数在Web应用中会被实际实现替代
    return 'approved'

def add_api_protection_delay():
    """
    添加10秒API保护延迟
    """
    print("🛡️ API保护延迟 - 等待10秒...")
    for i in range(10, 0, -1):
        print(f"⏱️ 倒计时: {i}秒")
        time.sleep(1)
    print("✅ API保护延迟完成") 