Thanks! I’ll dive into the Front REST API documentation to identify the key endpoints, message structures, and how to interact using both the official Python SDK (if available) and raw HTTP requests. I’ll also include what controls are available for fetching fresh emails, working with private/shared inboxes, tagging, moving, and assigning messages, along with a short overview of how to discover inboxes, tags, and team members.

I’ll let you know as soon as the detailed guide is ready.


# Using Front’s REST API for Email Retrieval and Management in Python

## Overview of Front’s API Data Model

Front organizes emails into **conversations**, which are threads of one or more **messages**. A conversation object includes metadata like an ID, subject line, status, assignee, tags, and the inboxes it appears in. For example, a conversation JSON might look like:

```json
{
  "id": "cnv_55c8c149",
  "subject": "You broke my heart, Hubert.",
  "status": "archived",
  "assignee": { "id": "tea_55c8c149", "email": "[email protected]", "first_name": "Leela", ... },
  "recipient": { "handle": "[email protected]", "role": "to", ... },
  "tags": [ { "id": "tag_55c8c149", "name": "Robots" } ],
  "inboxes": [ { "id": "inb_12345", "name": "Support" } ],
  ... 
}
```

*(Fields truncated for brevity)*. In this structure, `status` indicates if the conversation is **open**, **archived**, **snoozed**, etc. (per teammate). The `assignee` field (if present) links to a teammate object (Front user) assigned to the conversation. The `tags` field holds an array of tag objects applied to the conversation. A conversation’s `type` will be `"email"` for email threads (Front also supports other channel types).

Each **message** within a conversation has its own ID and content. Messages are either *inbound* (received) or *outbound* (sent). A message object includes fields like the sender/author, timestamp, and content. The content is usually available as text and/or HTML body, along with any attachments. For example, a message may have a `body` (the text or HTML content) and an `attachments` array for files. Note that by default, message bodies might be **lazy-loaded** – meaning the API may not return full content for older messages unless specifically fetched. To retrieve message contents, you’ll typically use the conversation messages endpoint (covered below).

## Retrieving “Fresh” Emails (Unread or Recent Conversations)

To fetch new or **unread** emails from both shared and private inboxes, you can use Front’s **Conversations** endpoints. The simplest approach is to use the **Search Conversations** API with appropriate filters. Front supports a rich query syntax for filtering conversations. For example:

* **Unread conversations**: Use the query filter `is:unread` to find conversations that contain unread messages.
* **Time-based filters**: Use `after:YYYY-MM-DD` or `before:YYYY-MM-DD` to filter by received date. For instance, `after:2025-06-01` would find conversations with messages after June 1, 2025.
* **Inbox-specific**: Use `inbox:<inbox name>` to limit results to a specific inbox (e.g. `inbox:Support` for the “Support” inbox). You can combine this with `is:unread` to get unread conversations in that inbox.
* **Other filters**: There are many others, like `is:assigned`/`is:unassigned` or `tag:<tag name>` etc., as documented in Front’s search syntax.

**Using the Search API:** The endpoint for searching conversations is:

```
GET https://api2.frontapp.com/conversations/search/{query}
```

where `{query}` is a URL-encoded search string with the filters above. For example, to get all unread conversations in all inboxes, you could GET:

```python
import requests

query = "is:unread"
url = f"https://api2.frontapp.com/conversations/search/{requests.utils.requote_uri(query)}"
resp = requests.get(url, headers={
    "Authorization": "Bearer <YOUR_API_TOKEN>",
    "Accept": "application/json"
})
convs = resp.json().get("_results", [])
for conv in convs:
    print(conv["id"], conv["subject"], conv["status"])
```

This returns a JSON with a list of conversation objects matching the query (and a total count). The results are ordered by last activity (most recently updated first). Using the `is:unread` filter ensures you catch any conversations with new, unread messages. You could also include a date filter (e.g. `after:2025-10-01 is:unread`) to refine “recently received” emails by date.

Alternatively, you can retrieve conversations per inbox. The **List conversations** endpoint returns all conversations company-wide in reverse chronological order, and the **List inbox conversations** endpoint returns conversations in a specific inbox. For example:

```
GET https://api2.frontapp.com/inboxes/<inbox_id>/conversations 
```

lists conversations in the given inbox (most recent first). However, these endpoints do not support filtering by unread status or time directly (“for more advanced filtering, see the search endpoint”), so using the search query as shown above is recommended for “fresh” emails.

After identifying relevant conversations, you may want to fetch the actual message contents for processing. To get all messages in a conversation (including their bodies and attachments), use:

```
GET https://api2.frontapp.com/conversations/<conversation_id>/messages
```

which returns an array of message objects in the conversation (newest first). In Python:

```python
conv_id = "cnv_55c8c149"  # example conversation ID
msg_url = f"https://api2.frontapp.com/conversations/{conv_id}/messages"
msg_resp = requests.get(msg_url, headers=headers)
messages = msg_resp.json()["_results"]
for msg in messages:
    print(msg["id"], msg["is_inbound"], msg.get("body", ""))
```

Each message will indicate whether it was inbound (`is_inbound=true`) or outbound, the sender/recipient info, timestamp, `body` content (if already available), etc. If the `body` is not immediately present, you may need to fetch the individual message via `GET /messages/<message_id>` or ensure the conversation has been opened by a teammate (which triggers content loading in Front’s backend).

**Note:** If you need *real-time* new email retrieval, consider using **Events** or **Webhooks**. The Core API offers a `List events` endpoint to poll for events like new inbound messages, and webhooks can push new message notifications to your app. However, for simplicity, periodic polling with the search API (e.g., checking for `is:unread` conversations every few minutes) is often sufficient.

## Tagging Conversations via API

After processing an email, you might want to tag the conversation for categorization or tracking. Tags in Front are applied at the **conversation level** (not per individual message). The relevant endpoint is:

```
POST https://api2.frontapp.com/conversations/<conversation_id>/tags
```

This **Add Conversation Tag** call will attach one or multiple tags to the conversation. You need to include in the request body the IDs of the tags to add. For example, if you have a tag ID `tag_abc123` (you can retrieve tag IDs via the tags list, described below), you would do:

```python
conv_id = "cnv_55c8c149"
url = f"https://api2.frontapp.com/conversations/{conv_id}/tags"
payload = { "tag_ids": ["tag_abc123"] }
resp = requests.post(url, json=payload, headers=headers)
if resp.status_code == 202:
    print("Tag added successfully.")
```

The API allows adding multiple tags in one call by sending an array of `tag_ids`. Similarly, to **remove** tags, use:

```
DELETE https://api2.frontapp.com/conversations/<conversation_id>/links
```

with a payload of tag IDs to remove (Front’s **Remove Conversation Tag** endpoint). *Example:* to remove the same tag:

```python
requests.delete(url, json={"tag_ids": ["tag_abc123"]}, headers=headers)
```

(front returns 204 No Content on success). After tagging, the conversation’s `tags` list will include the new tags, each with an `id` and `name`.

**Discovering Tag IDs:** You likely need to fetch existing tags and their IDs. Use the **List tags** endpoints to retrieve tags defined in your Front account. For company-wide tags (shared tags), call:

```
GET https://api2.frontapp.com/company/tags
```

which returns all tags in the company with their `id` and `name`. If you need personal (teammate-specific) tags, there are separate endpoints (e.g., teammate tags), but most tags used for categorizing conversations in shared inboxes are company tags. In the response, find the tag object by `name` and note its `id` for use in the tagging call.

For example, a tag object might look like: `{ "id": "tag_55c8c149", "name": "Robots", ... }`. To tag a conversation as “Robots”, you’d use that tag’s ID in the POST above.

## Moving Conversations to Specific Inboxes

Front does not provide a single direct “move conversation” API call in the Core API (since a conversation is usually tied to the channel/inbox it was originally received in). In Front’s model, a conversation can actually appear in **multiple inboxes** if it involves multiple recipients or has been shared. You can verify which inboxes a conversation resides in via:

```
GET https://api2.frontapp.com/conversations/<conversation_id>/inboxes
```

which lists the inboxes that conversation is in. However, programmatically moving a conversation from one inbox to another is not as straightforward as tagging or assigning. There is no direct “transfer” endpoint for inboxes in the Core API. Typically, an email conversation stays in the inbox (or inboxes) of the channel on which it was received.

**Workaround for moving**: If your workflow requires effectively moving an email to another team’s inbox or a private inbox, consider these approaches:

* **Assign to a teammate or group** – Often, in Front, moving an email to someone’s personal scope is handled by assignment (covered below) rather than physically moving it out of the shared inbox. The email stays in the shared inbox but is marked assigned to someone (and they can filter on their assigned conversations).
* **Using Tags or Archive** – Another pattern is to use tags or custom statuses to mark conversations as “handled” or to triage them into categories (instead of moving between inboxes). For example, apply a tag like “Escalated” or “Level2” and use that to indicate it needs attention from another team.
* **Copying content to another inbox** – In cases where you absolutely need the conversation under a different inbox (say, transferring to another team’s queue), a common method is to **forward or send** the message into the other inbox via the API. For instance, you could use the **Create Message** endpoint to send an internal email or forward to an address associated with the target inbox. This effectively creates a new conversation in the target inbox containing the original message content.

It’s important to note that Front’s API has a concept of **links**, but those “conversation links” are meant for linking a conversation to external objects (or merging conversations), not for inbox placement. So, while the UI may allow dragging a conversation to another inbox in some cases, the API user typically must resort to assignment or creating a new message in the desired inbox.

In summary, **there is no core-API endpoint to directly move a conversation to a different inbox**. Instead, use assignment or create a new conversation in the target inbox if needed.

## Assigning Conversations to Teammates or Groups

Assignment in Front means attributing the conversation to a specific teammate (individual) or to a **teammate group** (a group of users) for follow-up. The relevant endpoint is:

```
PUT https://api2.frontapp.com/conversations/<conversation_id>/assignee
```

– this will assign (or unassign) a conversation. You need to provide the assignee in the request body. The API accepts either a teammate’s ID or a group’s ID as the assignee. For example, to assign a conversation to a particular user:

```python
conv_id = "cnv_55c8c149"
assign_url = f"https://api2.frontapp.com/conversations/{conv_id}/assignee"
payload = { "assignee_id": "tea_123456" }  # teammate ID here
requests.put(assign_url, json=payload, headers=headers)
```

To assign to a group, you would use the group’s ID (which typically has a prefix like `grp_...` or similar). If the JSON body is left empty or `assignee_id` is `null`, the conversation will be unassigned (no owner).

A successful assignment returns a 204 No Content status. After assigning, the conversation’s `assignee` field will reflect the new teammate or group (with their details). In Front, if a conversation is assigned to a teammate who becomes unavailable, Front may auto-unassign it to keep it visible to others.

**Tip – Teammate and Group IDs**: To get a specific teammate’s ID, you can use the **List Teammates** endpoint. Calling:

```
GET https://api2.frontapp.com/teammates
```

will return an array of teammate objects (all users in your company) with their `id`, name, email, etc. Each teammate object looks like the example below, where `id` starts with `tea_`:

```json
{
  "id": "tea_55c8c149",
  "email": "[email protected]",
  "first_name": "Leela",
  "last_name": "Turanga",
  "is_admin": true,
  "is_available": true,
  ...
}
```

You can also use a teammate’s email as an **alias** for their ID in many API calls – for instance `"alt:email:[email protected]"` can be used anywhere a teammate ID is accepted. This can simplify assignments without needing to look up the exact ID. For groups, you can list teammate groups via the **List Teammate Groups** endpoint (if enabled on your Front account):

```
GET https://api2.frontapp.com/teams/<team_id>/teammate_groups 
```

(to list groups in a given team/workspace) or other related endpoints. Each group will have an `id` (often prefixed with `grp_`) and a name. Use that `id` as the `assignee_id` to assign a conversation to the whole group. When assigned to a group, the conversation will appear as unassigned (available) to all members of that group until one of them takes it, but it’s categorized under that group for reporting and workflow purposes.

## Discovering Inboxes, Tags, Teammates, and Groups via API

Before performing actions like moving or assigning, you often need to know the IDs of various resources:

* **List Inboxes**: To get all inboxes (shared or private) accessible in your account, use:

  ```
  GET https://api2.frontapp.com/inboxes
  ```

  This returns an array of inbox objects with fields like `id` (e.g. `inb_12345`), `name` (“Sales”, “Support”, or a teammate’s private inbox name), and other details. Shared team inboxes and private inboxes will all appear here with unique IDs. (Alternatively, `GET /teams/<team_id>/inboxes` lists inboxes for a specific team workspace, and `GET /teammates/<teammate_id>/inboxes` lists inboxes a particular user has access to.)

* **List Tags**: As mentioned, `GET /company/tags` lists all company-level tags. Each tag object has an `id`, `name`, and possibly an `owner` (the team that owns the tag). There are also endpoints for teammate-specific tags if needed (e.g., `GET /teammate_tags`), but for most cases you’ll use company tags.

* **List Teammates**: Use `GET /teammates` to fetch all users. From the result, you can find a teammate’s `id` (needed for assignments) by matching their email or name. For example, if “Alice” is a user, find the object with `"first_name": "Alice"` and grab the `"id": "tea_xxxxx"`.

* **List Teammate Groups**: If you utilize groups for assignment, you can list them. Depending on your account’s structure, you might list groups per team. For instance, `GET /teammate_groups` (or the relevant teams endpoint) will give you group IDs and names. If the API for groups is not enabled (Front’s teammate groups feature was introduced relatively recently), you may need to obtain the group IDs from the Front UI or use the Developer console. Each group’s `id` would be used for assignments as described.

With these discovery endpoints, you can dynamically fetch IDs rather than hard-coding them. For example, you might call `GET /inboxes` once and cache the mapping of inbox names to IDs, or call `GET /company/tags` to build a dictionary of tag names to IDs for your tagging logic.

## Using Python and the Front API

Front’s REST API is a standard JSON-over-HTTPS API. **Authentication** is done via an API token or OAuth; the simplest is to create a **API token** and use it as a Bearer token in the Authorization header. For example:

```python
headers = {
    "Authorization": "Bearer <YOUR_API_TOKEN>",
    "Accept": "application/json"
}
response = requests.get("https://api2.frontapp.com/conversations", headers=headers)
if response.status_code == 200:
    data = response.json()
    # ... process data ...
```

All the examples above assumed you have such a header prepared. Ensure that you set the `Accept: application/json` header as shown, and of course replace `<YOUR_API_TOKEN>` with your actual token. (You can generate an API token in Front’s settings; tokens may be scoped to specific teams or data access as needed.)

**Official SDKs**: As of now, Front does **not provide an official Python SDK** for the Core API. Instead, the official documentation provides example snippets in Python (as well as cURL, Node, Ruby, PHP) for each endpoint. You can copy these patterns using the `requests` library, as we have done in the examples. There is an open-source community library called **py-front** on PyPI, which provides a wrapper around the Front API, but it’s not officially maintained by Front. In many cases, using `requests` directly with the REST endpoints is straightforward and gives you full coverage of the API.

**Handling Pagination**: Many list endpoints (e.g. listing conversations or messages) return a limited set of results per page, with a pagination token in the response (`_pagination` field). To get all results, you’ll need to follow the `next` page token by adding `page_token=<token>` as a query parameter on subsequent requests. Keep this in mind especially when listing conversations or events, as “fresh” emails could be on subsequent pages if your account has high volume.

## Summary

Using Front’s REST API, you can retrieve emails as conversation threads, filter for fresh/unread items, process them, and then update their status in Front – tagging them, assigning them to the right person or group, and marking them done (archiving) or even moving them to another context if necessary. All core entities – conversations, messages, inboxes, tags, teammates – are accessible through the API with JSON endpoints. With Python’s `requests` and the patterns above, you can automate your Front workflows: for example, fetch new emails in a shared inbox, run your processing logic, then call the API to tag and assign those conversations, and perhaps archive or snooze them via `PATCH /conversations/<id>` (to mark as done). By leveraging the official Front documentation and endpoints, you ensure your integration stays in sync with Front’s data and actions.

**Sources:**

* Front API Reference – Conversations, Messages, Tags, Inboxes, etc.
* Front Help Center – Search query syntax and filters
* Front API Reference – Data models and examples (Conversation, Teammate objects, etc.)
* Front API Reference – Assignment, tagging, and other action endpoints.
