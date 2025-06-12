Below is a mapping of every Front endpoint, object, and Python tactic you’ll need for the three life-cycle steps you listed (fetch → process → write-back).  References point to the exact section of Front’s live docs so you can drill deeper if needed.

---

## 1 · Authentication & Scopes

| What                                                 | Endpoint / Concept                                     | Notes                                                                                                                                                    |
| ---------------------------------------------------- | ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Company-wide token (recommended for server jobs)** | Static API token created under *Settings → Developers* | One header: `Authorization: Bearer <token>`                                                                                                              |
| **OAuth token (multi-tenant or user-level)**         | Standard 3-leg OAuth2                                  | Token scopes determine what you can reach. <br/>`shared_resources` ⇒ all team inboxes, `private_resources` ⇒ personal inboxes, or a narrowed team scope. |

---

## 2 · Getting “fresh” email

### 2-A Polling endpoints

| Need                                           | Core endpoint                                | Key filters                                                                                                                                            |
| ---------------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Everything** (all conversations you can see) | `GET /conversations`                         | `updated_after=<unix-ms>` to delta-poll ([dev.frontapp.com][1])                                                                                        |
| **Single inbox (shared *or* private)**         | `GET /inboxes/{inbox_id}/conversations`      | Same filters; returns only that inbox ([dev.frontapp.com][2])                                                                                          |
| **Search across inboxes**                      | `GET /conversations/search/{query}`          | Supports Front search grammar (`is:archived`, `tag:foo`, etc.). Endpoint is proportionally rate-limited ([dev.frontapp.com][3], [dev.frontapp.com][4]) |
| **My assigned work queue**                     | `GET /teammates/{teammate_id}/conversations` | For per-agent polling dashboards ([dev.frontapp.com][5])                                                                                               |

> **Hints**  Set `limit=50` (the page max) and keep the `pagination_token` you get back to walk forward without missing anything.

### 2-B Real-time push

*Subscribe a webhook* to the **`inbound`** or **`move`** Events via *Settings → Integrations*. Your listener receives JSON payloads matching the schemas in the **Events** reference ([dev.frontapp.com][6]). That lets you process mail without polling at all.

### 2-C Message / Conversation JSON cheat-sheet

The two root objects you’ll handle:

| Field                | Conversation                                                        | Message                         |
| -------------------- | ------------------------------------------------------------------- | ------------------------------- |
| `id`                 | `cnv_<hash>`                                                        | `msg_<hash>`                    |
| `subject`            | ✅                                                                   | ✅                               |
| `status`             | `open` \| `archived` \| `spam` \| `trashed` ([dev.frontapp.com][7]) | —                               |
| `tags`               | `[tag_id, …]`                                                       | —                               |
| `assignee_id`        | Teammate or null                                                    | —                               |
| `links`              | array of channel links                                              | —                               |
| `is_inbound`         | —                                                                   | boolean ([dev.frontapp.com][8]) |
| `sender`, `to`, `cc` | —                                                                   | contact handles                 |
| `created_at`         | ms epoch                                                            | ms epoch                        |

---

## 3 · Writing back (tags, moves, assignments)

| Action                         | Endpoint                                                                                                                   | Payload                                                                            |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| **Add tag**                    | `POST /conversations/{id}/tags`                                                                                            | `{ "tag_id": "tag_abc" }` ([dev.frontapp.com][9])                                  |
| **Remove tag**                 | `DELETE /conversations/{id}/tags/{tag_id}`                                                                                 | —                                                                                  |
| **Create tag (if missing)**    | `POST /tags` (legacy) or `POST /company_tags`                                                                              | `{ "name": "New-Tag" }` ([dev.frontapp.com][10])                                   |
| **Assign / unassign**          | `PUT /conversations/{id}/assignee`                                                                                         | `{ "teammate_id": "team_123" }` ( use `null` to unassign ) ([dev.frontapp.com][4]) |
| **Move to another inbox**      | `PATCH /conversations/{id}` with body `{ "inbox_id": "inbox_456" }` – allowed per rate-limit table ([dev.frontapp.com][4]) |                                                                                    |
| **Bulk move (plugin context)** | `move(inboxId)` helper in the Plugin SDK for UI actions ([dev.frontapp.com][11])                                           |                                                                                    |

Rate caps for the heavy-write routes (`PATCH /conversations`, `PUT /.../assignee`, etc.) are 5 req · conversation⁻¹ · s⁻¹ ([dev.frontapp.com][4]).

---

## 4 · Python integration options

### 4-A Lightweight (direct `requests`)

```python
import os, time, requests

FRONT_TOKEN = os.environ["FRONT_TOKEN"]
BASE = "https://api2.frontapp.com"

def front_get(path, **params):
    r = requests.get(BASE + path,
                     headers={"Authorization": f"Bearer {FRONT_TOKEN}"},
                     params=params, timeout=10)
    r.raise_for_status()
    return r.json()

# 1. Fetch new conversations since last checkpoint
last_run = int(time.time()*1000) - 5*60*1000  # last 5 min
resp = front_get("/conversations", updated_after=last_run, limit=50)
for c in resp["_results"]:
    # pull first message in the thread
    msg = front_get(f"/conversations/{c['id']}/messages", limit=1)["_results"][0]
    # → hand off to your step 2 processor
    ...

# 3-a. apply a tag
def add_tag(c_id, tag_id):
    requests.post(
        f"{BASE}/conversations/{c_id}/tags",
        json={"tag_id": tag_id},
        headers={"Authorization": f"Bearer {FRONT_TOKEN}"},
        timeout=5,
    ).raise_for_status()
```

### 4-B Community wrapper (`py-front`)

```python
import front
front.set_api_key(FRONT_TOKEN)

# list open conversations
for conv in front.Conversation.objects.all():
    print(conv.id, conv.subject)

# tag and assign
conv = front.Conversation.get("cnv_abc")
conv.add_tag("tag_foo")
conv.assign("alt:email:agent@example.com")
```

Package is simple but stable; last published 0 Nov 2018 – fine for basic CRUD but lacks new endpoints, so fall back to raw calls when needed. ([pypi.org][12])

---

## 5 · Putting it together

1. **Bootstrap** – store `pagination_token` + `updated_after` cursor so each run only grabs unseen traffic.
2. **Process** – your own logic.
3. **Write-back** – use tag/assign/move calls above.
4. **Scale** – obey rate headers and consider webhooks for near-real-time workloads.

With those endpoints and object schemas you can cover private inboxes, every shared inbox, and any follow-up routing you need inside Front.

[1]: https://dev.frontapp.com/reference/list-conversations?utm_source=chatgpt.com "List conversations - Front APIs"
[2]: https://dev.frontapp.com/reference/inboxes?utm_source=chatgpt.com "Inboxes - Front APIs"
[3]: https://dev.frontapp.com/docs/search-1?utm_source=chatgpt.com "Search - Front APIs"
[4]: https://dev.frontapp.com/docs/rate-limiting?utm_source=chatgpt.com "Rate limits - Front APIs"
[5]: https://dev.frontapp.com/reference/list-assigned-conversations?utm_source=chatgpt.com "List assigned conversations - Front APIs"
[6]: https://dev.frontapp.com/reference/events?utm_source=chatgpt.com "Events - Front APIs"
[7]: https://dev.frontapp.com/reference/plugin-sdk-conversation?utm_source=chatgpt.com "Conversation - Front APIs"
[8]: https://dev.frontapp.com/reference/messages?utm_source=chatgpt.com "Messages - Front APIs"
[9]: https://dev.frontapp.com/reference/plugin-sdk-context?utm_source=chatgpt.com "Context - Front APIs"
[10]: https://dev.frontapp.com/reference/create-tag?utm_source=chatgpt.com "Create tag - Front APIs"
[11]: https://dev.frontapp.com/reference/move?utm_source=chatgpt.com "move - Front APIs"
[12]: https://pypi.org/project/py-front/?utm_source=chatgpt.com "py-front - PyPI"
