import os
from dotenv import load_dotenv

# Construct the path to the .env file in the project root
# __file__ is the path to the current file (aegis_system/__init__.py)
# os.path.dirname(__file__) is the 'aegis_system' directory
# os.path.join(..., '..') goes one level up to the project root
dotenv_path = os.path.join(os.path.dirname(__file__), '..', '.env')

# Load the .env file from the root
if os.path.exists(dotenv_path):
    print(f"📦 Loading .env file from: {dotenv_path}")
    load_dotenv(dotenv_path=dotenv_path)
else:
    print("⚠️ .env file not found in project root. Skipping.") 