# DeepSeek API configuration (deprecated — prefer .env; see legacy-python/README.md)
import os

deepseek_api_key: str = os.getenv("DEEPSEEK_API_KEY", "")
deepseek_base_url: str = os.getenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com")
deepseek_model: str = os.getenv("DEEPSEEK_MODEL", "deepseek-chat")
