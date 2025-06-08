from typing import TypedDict, List, Optional, Dict, Any

class AegisState(TypedDict, total=False):
    """
    神盾危机管理系统的共享状态 (v3.1 - 添加批准请求功能)
    """
    # 危机信息
    crisis_description: Optional[str]  # 当前危机状况的描述
    realtime_sentiment: Optional[str]  # 统一的实时舆情反馈
    cycles: int  # 危机处理周期数
    crisis_resolved: bool  # 危机是否已解决
    
    # 各智能体的分析结果
    technical_report: Optional[str]  # 技术诊断报告
    pr_strategy: Optional[Dict[str, Any]]  # 公关策略（JSON格式）
    legal_review: Optional[str]      # 法律审查结果
    
    # 指挥决策系统
    human_command: Optional[str]          # 人类指挥官的原始指令
    parsed_tasks: Optional[Dict[str, Any]]  # 解析后的任务分配
    executed_actions: Optional[Dict[str, Any]]  # 各部门执行结果
    department_execution: Dict[str, Any]    # 部门执行状态（新增）
    
    # 批准请求系统（新增）
    approval_requests: List[Dict[str, Any]]  # 智能体的批准请求列表
    pending_approval: bool                   # 是否有待批准的请求
    
    # 传统字段（保持兼容性）
    comprehensive_proposal: Optional[str] # 综合提案 (用于决策)
    human_decision: Optional[str]         # 人类或AI数字分身的决策
    action_plan: Optional[str]            # 跨部门行动计划
    
    # 系统日志
    incident_log: List[str]               # 事件日志（新）
    action_log: List[str]                 # 行动日志（保持兼容）
    post_mortem_report: Optional[str]     # 复盘报告

def create_initial_state() -> AegisState:
    """创建初始状态，确保所有必需字段都有默认值"""
    return {
        "crisis_description": None,
        "realtime_sentiment": None,
        "cycles": 0,
        "crisis_resolved": False,
        "technical_report": None,
        "pr_strategy": None,
        "legal_review": None,
        "human_command": None,
        "parsed_tasks": None,
        "executed_actions": None,
        "department_execution": {},
        "approval_requests": [],
        "pending_approval": False,
        "comprehensive_proposal": None,
        "human_decision": None,
        "action_plan": None,
        "incident_log": [],
        "action_log": [],
        "post_mortem_report": None
    } 