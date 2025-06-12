# Front API technical guide for email management

The Front Core API v2 provides comprehensive REST endpoints at `https://api2.frontapp.com` for managing emails programmatically. This guide covers all the technical details needed to build a Python program that retrieves emails, manages tags, moves messages between inboxes, and handles assignments.

## Authentication and setup fundamentals

Front supports two primary authentication methods for API access. **API tokens** work best for private integrations and can be created in Settings → Developers → API Tokens. Include your token in requests using the Authorization header: `Bearer [YOUR_API_TOKEN]`. For public integrations, Front requires **OAuth 2.0** authentication with appropriate scopes like `shared:conversations`, `shared:messages`, and `shared:tags` for email management operations.

The official Python SDK `py-front` simplifies implementation:

```python
pip install py-front

import front
front.set_api_key("your_jwt_token_here")

# List all conversations
for conv in front.Conversation.objects.all():
    print(conv.id, conv.subject)
```

For manual implementation, create a base client:

```python
import requests
import json

class FrontAPI:
    def __init__(self, api_token):
        self.base_url = "https://api2.frontapp.com"
        self.headers = {
            'Authorization': f'Bearer {api_token}',
            'Content-Type': 'application/json'
        }
```

## Retrieving emails from inboxes effectively

The primary method for email retrieval uses the conversations endpoint, which returns both individual emails and threaded conversations. To **list all conversations** across inboxes:

```http
GET https://api2.frontapp.com/conversations?limit=50
Authorization: Bearer {token}
```

For **inbox-specific retrieval**, including both private and shared inboxes:

```http
GET https://api2.frontapp.com/inboxes/{inbox_id}/conversations
```

The response includes a conversation object with **key fields**: `id`, `subject`, `status` (open/archived), `assignee`, and links to fetch full message content. To get **fresh or unread emails** specifically, use search queries:

```http
GET https://api2.frontapp.com/conversations/search/is:unread%20after:1609459200
```

Search supports powerful filtering with queries like `is:unread`, `is:open`, `after:{timestamp}`, and `from:{email}`. For real-time email updates, the Events API provides immediate notifications:

```http
GET https://api2.frontapp.com/events?q=type:inbound
```

Each conversation contains multiple messages accessible through:

```http
GET https://api2.frontapp.com/conversations/{conversation_id}/messages
```

The **message object structure** includes essential fields:

```json
{
  "id": "msg_55c8c149",
  "type": "email",
  "is_inbound": true,
  "created_at": 1453770984.123,
  "body": "Full HTML message body",
  "text": "Plain text version",
  "subject": "Email subject",
  "author": {
    "id": "tea_55c8c149",
    "email": "[email protected]"
  },
  "recipients": [
    {"handle": "[email protected]", "role": "to"}
  ]
}
```

## Managing tags on emails programmatically

Front's tagging system allows flexible email categorization. To **add tags to a conversation**:

```http
PUT https://api2.frontapp.com/conversations/{conversation_id}/tags
Content-Type: application/json

{
  "tag_ids": ["tag_123", "tag_456"]
}
```

This endpoint **replaces all existing tags**, so include both new and existing tags you want to keep. To create new tags before applying them:

```http
POST https://api2.frontapp.com/tags
Content-Type: application/json

{
  "name": "Priority",
  "color": "red"
}
```

The **tag object** contains:

```json
{
  "id": "tag_55c8c149",
  "name": "Priority", 
  "color": "red",
  "is_private": false,
  "created_at": 1453770984.123
}
```

In Python, tag management becomes straightforward:

```python
def add_tags(self, conversation_id, tag_ids):
    data = {'tag_ids': tag_ids}
    response = requests.put(
        f"{self.base_url}/conversations/{conversation_id}/tags",
        headers=self.headers,
        data=json.dumps(data)
    )
    return response.status_code == 204
```

## Moving emails between inboxes seamlessly

Email movement in Front involves updating the conversation's inbox assignment. To **move a conversation** to a different inbox:

```http
PATCH https://api2.frontapp.com/conversations/{conversation_id}
Content-Type: application/json

{
  "inbox_id": "inb_55c8c149"
}
```

For **archiving conversations** (removing from active inbox):

```http
PATCH https://api2.frontapp.com/conversations/{conversation_id}
Content-Type: application/json

{
  "status": "archived"
}
```

When **importing external emails** directly to a specific inbox:

```http
POST https://api2.frontapp.com/inboxes/{inbox_id}/imported_messages
Content-Type: application/json

{
  "sender": {
    "handle": "[email protected]",
    "name": "John Doe"
  },
  "to": ["[email protected]"],
  "subject": "Imported email",
  "body": "Email content here",
  "created_at": 1453770984.123
}
```

## Assignment to teams and individuals

Front handles team assignments through inbox routing and individual assignments through the assignee field. To **assign to an individual**:

```http
PATCH https://api2.frontapp.com/conversations/{conversation_id}
Content-Type: application/json

{
  "assignee_id": "tea_55c8c149"
}
```

For **team assignment**, move the conversation to a team's shared inbox:

```http
PATCH https://api2.frontapp.com/conversations/{conversation_id}
Content-Type: application/json

{
  "inbox_id": "inb_team_inbox_id"
}
```

To **unassign** a conversation, set assignee_id to null. The **teammate object** structure provides user details:

```json
{
  "id": "tea_55c8c149",
  "email": "[email protected]",
  "username": "john_doe",
  "first_name": "John",
  "last_name": "Doe",
  "is_available": true
}
```

## Rate limits and performance optimization

Front enforces **rate limits per company** based on your plan: Starter (50 rpm), Growth (100 rpm), Scale (200 rpm). Monitor these through response headers:

```
X-RateLimit-Remaining: 45
Retry-After: 60
```

**Best practices** for optimal performance include implementing exponential backoff for retries, using pagination with `page_token` for large datasets, caching frequently accessed data like teammate lists, and respecting the `Retry-After` header when rate limited.

## Webhook integration for real-time updates

Configure webhooks in Front Settings to receive real-time notifications for events like `inbound` (new emails), `assign`/`unassign` (assignment changes), `tag` (tag modifications), and `archive`/`reopen` (status changes).

The **webhook payload** includes:

```json
{
  "id": "evt_55c8c149",
  "type": "assign",
  "emitted_at": 1453770984.123,
  "conversation": {
    "id": "cnv_55c8c149",
    "_links": {
      "self": "https://api2.frontapp.com/conversations/cnv_55c8c149"
    }
  }
}
```

## Complete Python implementation example

Here's a comprehensive Python class implementing all required functionality:

```python
import requests
import json
from typing import List, Dict, Optional

class FrontEmailManager:
    def __init__(self, api_token: str):
        self.base_url = "https://api2.frontapp.com"
        self.headers = {
            'Authorization': f'Bearer {api_token}',
            'Content-Type': 'application/json'
        }
    
    def get_fresh_emails(self, inbox_id: Optional[str] = None, 
                        unread_only: bool = True) -> List[Dict]:
        """Retrieve fresh emails from inbox"""
        if inbox_id:
            url = f"{self.base_url}/inboxes/{inbox_id}/conversations"
        else:
            url = f"{self.base_url}/conversations"
        
        params = {}
        if unread_only:
            params['q'] = 'is:unread'
        
        response = requests.get(url, headers=self.headers, params=params)
        return response.json().get('_results', [])
    
    def add_tags_to_email(self, conversation_id: str, 
                         tag_ids: List[str]) -> bool:
        """Add tags to email conversation"""
        url = f"{self.base_url}/conversations/{conversation_id}/tags"
        data = {'tag_ids': tag_ids}
        
        response = requests.put(url, headers=self.headers, 
                              data=json.dumps(data))
        return response.status_code == 204
    
    def move_to_inbox(self, conversation_id: str, 
                     inbox_id: str) -> bool:
        """Move email to different inbox"""
        url = f"{self.base_url}/conversations/{conversation_id}"
        data = {'inbox_id': inbox_id}
        
        response = requests.patch(url, headers=self.headers,
                                data=json.dumps(data))
        return response.status_code == 204
    
    def assign_to_user(self, conversation_id: str, 
                      assignee_id: str) -> bool:
        """Assign email to specific user"""
        url = f"{self.base_url}/conversations/{conversation_id}"
        data = {'assignee_id': assignee_id}
        
        response = requests.patch(url, headers=self.headers,
                                data=json.dumps(data))
        return response.status_code == 204

# Usage example
manager = FrontEmailManager("your_api_token")
emails = manager.get_fresh_emails(unread_only=True)
for email in emails:
    manager.add_tags_to_email(email['id'], ['tag_priority'])
    manager.move_to_inbox(email['id'], 'inb_support')
```

This comprehensive technical guide provides all the API endpoints, authentication methods, object structures, and implementation details needed to build a robust email management program using Front's API. The combination of the official SDK and custom implementation options gives you flexibility in how you integrate these capabilities into your application.

# Front API Technical Guide - Citations Appendix

This document contains all the source citations referenced in the Front API Technical Guide for Email Management.

## Citations List

1. **Front Platform - Introduction**
   - URL: https://dev.frontapp.com/reference/introduction
   - Used for: Basic API information and base URL details

2. **Front Platform - Authentication**
   - URL: https://dev.frontapp.com/docs/authentication
   - Used for: Authentication methods, API tokens, and OAuth 2.0 implementation details

3. **py-front - PyPI**
   - URL: https://pypi.org/project/py-front/
   - Used for: Python SDK installation and basic usage information

4. **GitHub - tizz98/py-front**
   - URL: https://github.com/tizz98/py-front
   - Used for: Python SDK documentation and examples

5. **Pipedream - List Conversations with Front API**
   - URL: https://pipedream.com/integrations/list-conversations-with-front-api-on-new-task-instant-from-awork-api-int_0GsjYGMM
   - Used for: Conversation listing examples

6. **Front Platform - Conversations**
   - URL: https://dev.frontapp.com/reference/conversations
   - Used for: Conversation endpoints, object structures, and search functionality

7. **Front Platform - Response Body Structure**
   - URL: https://dev.frontapp.com/reference/response-body-structure
   - Used for: Understanding API response formats

8. **Front Platform - Events**
   - URL: https://dev.frontapp.com/reference/events
   - Used for: Events API for real-time updates and webhook information

9. **Front Platform - Creating a Custom Channel**
   - URL: https://dev.frontapp.com/docs/creating-a-custom-channel
   - Used for: Channel creation concepts

10. **Front Platform - Messages**
    - URL: https://dev.frontapp.com/reference/messages
    - Used for: Message object structure and message retrieval endpoints

11. **Help Scout Developers - Update Tags** (Note: This was incorrectly referenced in search results)
    - URL: https://developer.helpscout.com/mailbox-api/endpoints/conversations/tags/update/
    - Not used in final guide (different platform)

12. **Front Platform - Import message**
    - URL: https://dev.frontapp.com/reference/import-inbox-message
    - Used for: Importing external emails to Front inboxes

13. **Front - How to assign a conversation**
    - URL: https://help.front.com/t/y724vf/how-to-assign-a-conversation
    - Used for: Assignment concepts and team routing

14. **Front Platform - Teammates**
    - URL: https://dev.frontapp.com/reference/teammates
    - Used for: Teammate object structure

15. **Front Platform - Rate limits**
    - URL: https://dev.frontapp.com/docs/rate-limiting
    - Used for: Rate limiting information by plan tier

16. **Front Platform - Front APIs**
    - URL: https://dev.frontapp.com/docs/welcome
    - Used for: General API overview and capabilities

17. **Front - Overview of Front's API and use cases**
    - URL: https://help.front.com/en/articles/2482
    - Used for: Understanding common API use cases

## Additional Resources

While not directly cited, these resources may be helpful for further implementation:

- Front API Reference Documentation: https://dev.frontapp.com/reference
- Front Developer Portal: https://dev.frontapp.com
- Front Support Documentation: https://help.front.com
- Front Community Forum: https://community.front.com

## Note on Citations

All citations were retrieved and verified during the research phase. The information has been synthesized and presented in the main technical guide to provide a comprehensive overview of Front's API capabilities for email management tasks.