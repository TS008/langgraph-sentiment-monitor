import random

class SimulatedMemory:
    """
    一个简单的、基于内存的模拟长期记忆模块。
    它在系统运行时记住经验，但关闭后会清空。
    """
    def __init__(self):
        self.experiences = []
        print("🧠 模拟长期记忆模块已初始化")

    def add_experience(self, crisis: str, decision: str, outcome: str):
        """
        记录一条经验。
        - crisis: 危机描述
        - decision: 做出的决策
        - outcome: 结果 ('resolved' 或 'escalated')
        """
        if not crisis or not decision:
            return
            
        experience_log = f"经验：对于危机'{crisis[:20]}...'，我们采取了'{decision[:20]}...'，结果是'{outcome}'。"
        self.experiences.append({
            "log": experience_log,
            "crisis": crisis,
            "decision": decision,
            "outcome": outcome
        })
        print(f"💡 新经验已存入记忆: {experience_log}")

    def retrieve_experiences(self, current_crisis: str, k: int = 3) -> str:
        """
        基于当前危机，提取最相关的k条经验。
        （模拟版：随机提取k条经验）
        """
        if not self.experiences:
            return "无历史经验可参考。"
        
        # 模拟版：随机选择一些经验作为参考
        num_to_retrieve = min(k, len(self.experiences))
        retrieved = random.sample(self.experiences, num_to_retrieve)
        
        if not retrieved:
            return "无历史经验可参考。"
            
        logs = [exp['log'] for exp in retrieved]
        print(f"📚 从记忆中提取了 {len(logs)} 条相关经验。")
        return "\n".join(logs)

# 创建一个全局的模拟记忆实例
memory_instance = SimulatedMemory() 