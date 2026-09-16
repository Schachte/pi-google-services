---
description: "Gmail, Calendar, Tasks, Drive & Contacts MCP tools for Pi (OAuth, single binary)"
---

# pi-google-services

Google Calendar & Gmail MCP server for Pi. Lets your Pi agent manage your calendar and emails through natural language.

## Setup

```bash
pi-google-services login    # Opens browser → authorize with Google
pi-google-services serve    # Start MCP server (done automatically by Pi)
```

## Calendar tools

### `list-events`
Show events in a date range.

Examples:
- "show me tomorrow's events"
- "what do I have this week?"
- "events from June 20 to June 25"

Arguments:
- `timeMin` (ISO 8601) — start of range (default: today 00:00)
- `timeMax` (ISO 8601) — end of range (default: today 23:59)
- `calendarId` — calendar to query (default: primary)
- `maxResults` — max events (default: 50)

### `create-event`
Create a new calendar event with optional attendees and Google Meet.

Examples:
- "create a meeting tomorrow at 3 PM"
- "schedule a call with juan@gmail.com on Thursday at 10 AM"
- "create an all-day event 'Birthday' on 12/25"
- "create a virtual meet tomorrow at 4 PM with Meet"

Arguments:
- `summary` (required) — event title
- `startTime` (required) — ISO 8601 start
- `endTime` (required) — ISO 8601 end
- `attendees` — comma-separated emails to invite
- `withMeet` (boolean) — add Google Meet link
- `description` — event description
- `location` — event location
- `calendarId` — target calendar (default: primary)

### `update-event`
Modify an existing event.

Examples:
- "move tomorrow's meeting to 4 PM"
- "rename the dinner event to 'Dinner with friends'"

Arguments:
- `eventId` (required) — event to modify
- `summary`, `startTime`, `endTime` — fields to update
- `calendarId` — target calendar (default: primary)

### `delete-event`
Remove an event.

Examples:
- "delete the test event on Friday"
- "remove the 3 PM meeting"

Arguments:
- `eventId` (required) — event to delete
- `calendarId` — target calendar (default: primary)

### `search-events`
Search events by text.

Examples:
- "search for events about 'meeting'"
- "find when I talked about 'presentation'"

Arguments:
- `query` (required) — text to search
- `maxResults` — max results (default: 50)

### `list-calendars`
List all available calendars.

Examples:
- "show me my calendars"
- "what calendars do I have?"

### `get-freebusy`
Check availability across calendars.

Examples:
- "am I free tomorrow at 3 PM?"
- "what times am I busy this week?"

Arguments:
- `timeMin`, `timeMax` — range to check
- `calendarIds` — comma-separated calendar IDs

## Gmail tools

### `list-inbox`
List recent inbox messages.

Examples:
- "show me my emails"
- "what's in my inbox?"
- "show me the last 5 LinkedIn emails"

Arguments:
- `maxResults` — max emails (default: 20)
- `query` — optional Gmail search filter
- `pageToken` — pagination token to get the next page (from a previous result)

### `get-email`
Read a full email by ID.

Examples:
- "read the first email in the list"
- "show me the full content of belo's email"

Arguments:
- `id` (required) — email message ID

### `search-emails`
Search emails with Gmail syntax across the whole mailbox (sent, inbox, etc.).

Examples:
- "search for emails from belo"
- "find emails about 'Uber' from this week"
- "show me unread emails"
- "show me emails I sent in August" (`query: in:sent after:2026/08/01`)

Arguments:
- `query` (required) — Gmail search query
- `maxResults` — max results (default: 20)
- `pageToken` — pagination token to get the next page (from a previous result)

### `send-email`
Send a new email with optional file attachments.

Examples:
- "send an email to lucsk94@gmail.com with subject 'Test' and body 'Hi, this is a test'"
- "send an email to juan@mail.com saying the meeting was moved to Friday"
- "send an email to maria@work.com with the PDF /home/user/report.pdf attached"

Arguments:
- `to` (required) — recipient email
- `subject` (required) — email subject
- `body` — email body text
- `attachments` — array of files to attach. Each item can have `localPath` (local file) or `driveFileId` (Google Drive file ID)

### `reply-to-email`
Reply to an existing thread with optional file attachments.

Examples:
- "reply to Natalia's email accepting the invitation"
- "reply to belo's email saying I already saw it"
- "reply to the sales email attaching /home/user/quote.pdf"

Arguments:
- `threadId` (required) — thread to reply to
- `to` (required) — recipient email
- `subject` (required) — reply subject
- `body` — reply body text
- `attachments` — array of files to attach. Each item can have `localPath` (local file) or `driveFileId` (Google Drive file ID)

## Tasks tools

### `list-tasklists`
Show all task lists.

Examples:
- "show me my task lists"

### `list-tasks`
List tasks (pending, completed, or all).

Examples:
- "show me my pending tasks"
- "what tasks do I have to do?"
- "show me completed tasks"

Arguments:
- `taskListId` — task list ID (default: @default)
- `status` — filter: pending, completed, or '' for all
- `maxResults` — max results (default: 50)

### `create-task`
Create a new task.

Examples:
- "create a task to buy milk"
- "schedule 'call the dentist' for tomorrow"

Arguments:
- `title` (required) — task title
- `taskListId` — target list (default: @default)
- `notes` — optional description
- `dueDate` — due date (YYYY-MM-DD)

### `complete-task`
Mark a task as done.

Examples:
- "mark the milk task as done"
- "complete the dentist task"

Arguments:
- `taskId` (required) — task to complete
- `taskListId` — target list (default: @default)

### `delete-task`
Delete a task.

Examples:
- "delete the test task"

Arguments:
- `taskId` (required) — task to delete
- `taskListId` — target list (default: @default)
