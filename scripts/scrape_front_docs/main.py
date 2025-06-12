import asyncio
import os
import argparse
from dotenv import load_dotenv
import openai
import anthropic
import google.generativeai as genai
from browser_use import Agent, Browser, BrowserConfig, BrowserContextConfig
from browser_use.browser.context import BrowserContext
import sys
import json
from pathlib import Path

# Load environment variables from .env file
load_dotenv()

# Default entry URL
ENTRY_URL = "https://academy.front.com/courses/54117e96-58bd-442f-995a-fb1de30af2ee/activities/dc2d5005-5095-4fa5-87ad-aaaf012d0244"

async def main():
    # Load input JSON
    input_path = Path(sys.argv[1])
    inp = json.loads(input_path.read_text())
    # inp = {"model": ..., "provider": ...}

    # Use inp["model"] and inp["provider"] instead of argparse
    model = inp["model"]
    provider = inp["provider"]

    # Ensure API key is set based on provider
    if provider == 'openai' and not os.getenv('OPENAI_API_KEY'):
        raise ValueError("OPENAI_API_KEY not found in environment variables. Please set it in your .env file.")
    elif provider == 'anthropic' and not os.getenv('ANTHROPIC_API_KEY'):
        raise ValueError("ANTHROPIC_API_KEY not found in environment variables. Please set it in your .env file.")
    elif provider == 'google' and not os.getenv('GEMINI_API_KEY'):
        raise ValueError("GEMINI_API_KEY not found in environment variables. Please set it in your .env file.")
    elif provider == 'deepseek' and not os.getenv('DEEPSEEK_API_KEY'):
        raise ValueError("DEEPSEEK_API_KEY not found in environment variables. Please set it in your .env file.")
    elif provider == 'grok' and not os.getenv('GROK_API_KEY'):
        raise ValueError("GROK_API_KEY not found in environment variables. Please set it in your .env file.")

    # Initialize the LLM based on provider
    if provider == 'openai':
        client = openai.OpenAI(api_key=os.getenv('OPENAI_API_KEY'))
        llm = client
    elif provider == 'anthropic':
        client = anthropic.Anthropic(api_key=os.getenv('ANTHROPIC_API_KEY'))
        llm = client
    elif provider == 'google':
        genai.configure(api_key=os.getenv('GEMINI_API_KEY'))
        
        google_model_name = model
        # If the provider is Google and the model argument is still the global default 'gpt-4',
        # then switch to the Google-specific default model.
        if model == 'gpt-4': 
            google_model_name = "gemini-2.5-flash-preview-04-17"
            print(f"Provider is Google. No specific model provided via --model, using default Google model: {google_model_name}")
        
        llm = genai.GenerativeModel(model_name=google_model_name)
    elif provider == 'deepseek':
        # Assuming DeepSeek uses an OpenAI-compatible API.
        # You might need to provide a base_url for the OpenAI client, e.g., base_url="https://api.deepseek.com/v1"
        print("Note: For DeepSeek provider, ensure DEEPSEEK_API_KEY is set. You might need to configure a base_url if using the OpenAI client.")
        client = openai.OpenAI(api_key=os.getenv('DEEPSEEK_API_KEY')) # Consider adding base_url if needed
        llm = client
    elif provider == 'grok':
        # Grok typically uses its own SDK (pip install groq).
        # Using the OpenAI client here is a placeholder and likely incorrect unless Grok offers an OpenAI-compatible endpoint.
        print("Warning: Grok provider is using OpenAI client as a placeholder. This may not work correctly. Consider integrating the 'groq' SDK.")
        client = openai.OpenAI(api_key=os.getenv('GROK_API_KEY')) # This is a placeholder
        llm = client
    else:
        raise ValueError(f"Unsupported provider: {provider}")

    # Initialize the browser with default configuration
    browser = Browser(config=BrowserConfig(headless=False))

    # Create a browser context
    async with BrowserContext(browser=browser, config=BrowserContextConfig()) as context:
        # Navigate to the entry URL
        await context.navigate_to(ENTRY_URL)
        current_page = await context.get_current_page()
        title = await current_page.title()
        url = current_page.url
        print(f"Page Title: {title}")
        print(f"Page URL: {url}")

        # Provision an Agent (optional: use the Agent for further automation)
        agent = Agent(
            task="Scrape Front Academy entry page",
            llm=llm,
            browser=browser,
            browser_context=context
        )
        print("Agent provisioned successfully.")

if __name__ == "__main__":
    asyncio.run(main())
