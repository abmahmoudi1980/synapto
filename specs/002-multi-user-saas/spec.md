# Feature Specification: Multi-User Telegram News Aggregator

**Feature Branch**: `002-multi-user-saas`

**Created**: 2026-06-23

**Status**: Draft

**Input**: User description: "I have an existing Telegram news aggregator system. Currently, it has a single-admin panel. The system scrapes messages from pre-defined Telegram channels and forwards all of them. I want to refactor this into a multi-user (SaaS-like) system where any user can log in via Telegram, manage their own monitored channels, and set custom AI-powered filters for each channel."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Log In with Telegram and Reach the Dashboard (Priority: P1)

A new visitor opens the service's web dashboard, sees a "Continue with Telegram" button, completes the Telegram-hosted confirmation, and is signed in as a recognized user with a fresh, empty dashboard they can use immediately.

**Why this priority**: Without authentication, no per-user state is possible and none of the downstream user stories can be exercised. This is the entry point of the SaaS model and the single capability the prior system did not need to provide.

**Independent Test**: Can be fully tested by clicking the login button, completing the Telegram confirmation, and verifying the user lands on a dashboard that shows their name and an empty channel list — with no other story implemented, the visitor can still log in and see "no channels yet."

**Acceptance Scenarios**:

1. **Given** an unauthenticated visitor opens the dashboard, **When** they click the "Continue with Telegram" button and confirm in the Telegram popup, **Then** the dashboard reloads showing the visitor's Telegram first name, an empty channel list, and the user remains signed in on subsequent page loads without re-confirming.
2. **Given** an unauthenticated visitor opens the dashboard, **When** the Telegram confirmation popup reports a tampered or stale payload, **Then** the system refuses the login and shows a clear error message — no account is created, no session is issued.
3. **Given** an authenticated user has not used the service for longer than the session lifetime, **When** they next open the dashboard, **Then** they are prompted to log in again with Telegram.
4. **Given** an authenticated user clicks "Log out", **When** the next request is made, **Then** the session is invalidated and the user is shown the login page again.

---

### User Story 2 - Subscribe to a Telegram Channel (Priority: P1)

A logged-in user adds a public Telegram channel by its handle, sees it appear in their personal watch-list, and can later remove it. The watch-list is private to the user; another user of the same service does not see the same channel unless they too have added it.

**Why this priority**: The watch-list is the input to every other downstream behavior (filtering, delivery, history). Without the ability to add and remove channels, the user receives nothing. This is the smallest unit of value after login.

**Independent Test**: Can be fully tested by logging in, submitting the handle of a public Telegram channel, verifying the channel appears in the user's list, then deleting it and verifying the list returns to empty. With no other story implemented, a user can still add and remove channels.

**Acceptance Scenarios**:

1. **Given** a logged-in user is on the channels page, **When** they submit the handle of a public Telegram channel that the bot can read, **Then** the channel appears in the user's list with the channel's display name and handle.
2. **Given** a logged-in user is on the channels page, **When** they submit a handle that does not exist on Telegram or that the bot cannot read, **Then** the system rejects the entry with a clear explanation and does not add it.
3. **Given** a logged-in user has one or more channels in their list, **When** they remove a channel, **Then** the channel disappears from their list immediately and the change persists across page reloads and service restarts.
4. **Given** two users of the service are both logged in, **When** user A subscribes to a channel that user B has not subscribed to, **Then** user A's list contains the channel and user B's list does not.
5. **Given** a logged-in user submits a handle that has already been added by another user, **When** they submit it, **Then** the channel becomes available to the second user without duplicating the underlying channel record.

---

### User Story 3 - Set a Custom AI-Powered Filter on a Subscribed Channel (Priority: P1)

A logged-in user opens a channel they subscribe to and configures a personal filter that decides which messages from that channel they receive. The filter can be simple keywords, a free-form natural-language rule, or a single category. The same channel can have a different filter for each user.

**Why this priority**: This is the headline new capability that distinguishes the SaaS from the prior system. Without it, the system would be a re-skinned version of the old one.

**Independent Test**: Can be fully tested by subscribing to a known channel, setting a keyword filter, observing that matching messages are delivered and non-matching messages are not. With delivery partially implemented, the user can still set and view filters in their dashboard.

**Acceptance Scenarios**:

1. **Given** a logged-in user has subscribed to a channel, **When** they set a keyword filter containing a comma-separated list (e.g. "btc, eth, sec") on that channel, **Then** the filter is saved and visible in the channel's settings.
2. **Given** a logged-in user has a keyword filter set on a channel, **When** a new message arrives on that channel whose text contains one of the configured keywords, **Then** the user receives the message in their Telegram chat. **When** a message arrives whose text contains none of the keywords, **Then** the user does not receive it.
3. **Given** a logged-in user has subscribed to a channel, **When** they set a natural-language filter prompt (e.g. "Only forward crypto news related to Ethereum and ignore Bitcoin"), **Then** the filter is saved and visible in the channel's settings.
4. **Given** a logged-in user has a natural-language filter set, **When** new messages arrive on that channel, **Then** the system asks the AI to decide per-message whether the message matches the user's rule, delivers matching messages to the user's chat, and silently drops non-matching ones.
5. **Given** a logged-in user has subscribed to a channel, **When** they set a single-category filter, **Then** only messages classified into that category are delivered to them.
6. **Given** a logged-in user has a filter set on a channel, **When** they update the filter, **Then** the new rule applies to the next batch of messages, and previous decisions are not retroactively changed.
7. **Given** a logged-in user has subscribed to multiple channels, **When** they set a "default" filter that is not tied to a specific channel, **Then** the default filter applies to any channel they have not overridden with a channel-specific filter.

---

### User Story 4 - Receive a Filtered Message in Their Telegram Chat (Priority: P2)

A logged-in user who has set a filter on a channel receives a Telegram message from the service's bot in their private chat when a source-channel message matches their filter. The delivered message tells the user what was forwarded and where it came from.

**Why this priority**: This is the actual value the user came for. The earlier stories (login, subscribe, filter) are inputs; this is the output. It is P2 because the system can demonstrate correct filter selection and storage without delivery working, but a non-delivering system is useless.

**Independent Test**: Can be fully tested by setting up a source channel that posts a known message, configuring a user with a permissive filter on that channel, and verifying the user receives a Telegram message in their private chat within one delivery cycle. With this story alone, the user can receive messages from one channel with one filter.

**Acceptance Scenarios**:

1. **Given** a logged-in user is subscribed to a channel and has a permissive filter, **When** a new message is posted to the source channel, **Then** the user receives a Telegram message in their private chat from the service's bot within the next delivery cycle, with a summary of the source message and a permalink to the source.
2. **Given** two users are both subscribed to the same channel, **When** a single new message is posted to that channel, **Then** each user receives their own Telegram message, with a summary tailored to that user's filter (when relevant) and the same permalink.
3. **Given** a logged-in user is subscribed to a channel and has a strict filter, **When** a new message is posted that does not match, **Then** the user does not receive any Telegram message for that source message.
4. **Given** a logged-in user has blocked the service's bot, **When** the next delivery cycle runs, **Then** the system records a delivery failure for that user, does not retry that user, and surfaces the failure in the user's dashboard.

---

### User Story 5 - Manage Personal Settings (Digest Cadence, Uncategorized Label) (Priority: P3)

A logged-in user can change the cadence at which they receive a delivery, customize the label used for messages that don't fit any category, and pick between a bundled or per-message delivery format. Settings are private to the user.

**Why this priority**: Quality-of-life and personalization. The system delivers value with default settings; these are refinements.

**Independent Test**: Can be fully tested by changing the digest interval in the user's settings and observing the new cadence take effect. With no other story implemented, the user can view and change their settings.

**Acceptance Scenarios**:

1. **Given** a logged-in user opens their settings page, **When** they change the digest interval, **Then** the next delivery cycle uses the new cadence and the change persists across sessions.
2. **Given** a logged-in user opens their settings page, **When** they change the uncategorized label, **Then** the new label is used in any subsequent delivery that contains an uncategorized item.
3. **Given** a logged-in user opens their settings page, **When** they change the delivery format between "bundled" (one message per cycle) and "per-message" (one message per post), **Then** the new format takes effect on the next cycle.

---

### Edge Cases

- **Login replay**: a captured Telegram sign-in payload is replayed within the freshness window. The system must detect the replay and refuse.
- **Cross-channel duplicates**: the same message text appears in two of the user's subscribed channels. The user receives at most one Telegram message per unique content.
- **Bot blocked by user**: the user has blocked the service's bot. The system must record the failure, stop retrying that user, and surface the failure in their dashboard.
- **AI service unavailable**: when the AI service is down, the system must degrade gracefully — either retry the message on the next cycle, deliver a degraded version, or skip the message — and never silently lose it.
- **Concurrent login from same user**: a user logs in from two browsers. Both sessions remain valid until either logs out or the session expires.
- **User deletes account / data**: a user requests account deletion. All per-user state (watch-list, filters, deliveries) is removed; shared system state (the global channel catalog) is preserved.
- **Empty category set**: the user has removed all categories. Messages can still be delivered using the user's uncategorized label.
- **Channel goes private / bans the bot**: the user keeps the channel in their list (with a status indicator) but receives no further messages from it. They can manually remove it.
- **Filter that matches nothing for a long time**: the user has a strict filter on a chatty channel. The system must not send them "no new items" messages when they have not actually received anything.
- **New user has no subscriptions**: a freshly-logged-in user has no channels. The dashboard is empty, no deliveries happen, no errors appear.
- **Per-user settings change while a cycle is running**: the user changes their interval mid-cycle. The current cycle uses the prior snapshot; the new interval takes effect on the next cycle.
- **Bot token rotation by operator**: the operator rotates the service's bot token. All logged-in users remain logged in (their sessions are independent of the bot token), and deliveries continue to work.
- **Operator system-default filter**: the operator has set a system-wide default filter (e.g. "ignore ads"). New users inherit this filter and can override it.
- **Many users, one channel, one cycle**: a single channel posts one message and 1,000 users are subscribed. The system must process and deliver to all 1,000 within one cycle without dropping any.

## Requirements *(mandatory)*

### Functional Requirements

#### Authentication & Sessions

- **FR-001**: The system MUST let a visitor log in by completing the Telegram-hosted confirmation popup on the dashboard. The system MUST verify the payload's signature against the service's bot token and reject any payload older than 5 minutes.
- **FR-002**: The system MUST persist user records keyed by the Telegram user identifier, including first name, last name (when present), username (when present), profile photo URL (when present), and a server-assigned record identifier.
- **FR-003**: The system MUST issue a session token upon successful login. The session MUST expire no later than 12 hours after issuance. The system MUST support invalidating a session out-of-band (operator action or user logout) so that the next request with that session is rejected.
- **FR-004**: The system MUST require a valid session for every dashboard and API call except public health probes. A missing or invalid session MUST result in a clear "please log in" response.
- **FR-005**: The system MUST NOT store any Telegram-issued secret in its own database; verification happens against the operator-supplied bot token at request time.

#### User & Channel Management

- **FR-006**: The system MUST let a logged-in user add a public Telegram channel by its handle, validate the handle against the live Telegram API, and add the channel to that user's personal watch-list.
- **FR-007**: The system MUST let a logged-in user list the channels on their personal watch-list and remove a channel from that list. Removing a channel MUST NOT affect other users who have the same channel in their list.
- **FR-008**: The system MUST persist the watch-list across restarts and reload it at the next login.
- **FR-009**: A single Telegram channel record MUST be shared across all users who add it; the system MUST NOT create duplicate records for the same handle.

#### Filters

- **FR-010**: The system MUST let a logged-in user set a per-channel filter of one of three types: keywords (comma-separated list, case-insensitive substring match against message text), natural-language prompt (a free-text rule evaluated by the AI per message), or single category (a single category name from the global category set).
- **FR-011**: A user MUST be able to set a "default" filter (not tied to a specific channel) that applies to all their channels unless overridden by a channel-specific filter.
- **FR-012**: The system MUST save, list, update, and delete filter rules. Deleted filters MUST take effect from the next message batch; in-flight cycles MAY finish with the prior rule.
- **FR-013**: A user MUST be able to disable a filter without deleting it (an "active" toggle), so they can re-enable it later.

#### Message Processing & Delivery

- **FR-014**: The system MUST fetch new messages from each subscribed channel on a recurring cycle, deduplicating across channels within a cycle, and MUST NOT re-deliver a message that has already been delivered to a user.
- **FR-015**: For each new message, the system MUST determine the set of users subscribed to that channel and run the user's effective filter against the message. The system MUST deliver the message to the user when the filter matches, and MUST NOT deliver when it does not.
- **FR-016**: For natural-language filters, the system MUST invoke the AI service with the message content and the user's prompt and use the AI's decision (match / no match) and (when matching) a one-line summary tailored to the user's interest. The system MUST treat a missing or unavailable AI as a temporary failure and not silently drop the message.
- **FR-017**: For keyword filters, the system MUST perform the match locally without invoking the AI service.
- **FR-018**: For category filters, the system MUST use the global category assigned to the message by the standard summarization step and compare it to the configured category name.
- **FR-019**: The system MUST deliver each matched message to the user in the user's private Telegram chat, from the service's designated bot, with a summary tailored to the user and a permalink to the source message.
- **FR-020**: The system MUST use the user's per-user delivery format (bundled or per-message) and digest cadence when delivering.
- **FR-021**: The system MUST record, for each (user, message, cycle), whether the message was matched, sent, filtered, or failed. This record MUST be visible to the user in their history view.
- **FR-022**: The system MUST respect Telegram's per-chat and global rate limits, including honoring any back-off interval Telegram returns.
- **FR-023**: The system MUST handle a Telegram "bot was blocked" response by marking that user's future deliveries as failed and surfacing the issue in the user's dashboard; the system MUST continue delivering to other users.
- **FR-024**: The system MUST allow an operator to set a system-wide default filter that is applied to every new user at the time of their first login, overridable by the user.

#### Personal Settings

- **FR-025**: The system MUST let a logged-in user set their personal digest cadence (in seconds, bounded between 1 minute and 24 hours), their uncategorized label, and their preferred delivery format. Settings MUST be private to the user.

#### Security & Isolation

- **FR-026**: The system MUST enforce that a user can read and modify only their own watch-list, filters, settings, and delivery history. Cross-user reads or modifications MUST be rejected as not-found or forbidden.
- **FR-027**: The system MUST treat the bot token and AI provider credentials as operator secrets, never expose them in API responses, and never log them in plaintext.

### Key Entities *(include if feature involves data)*

- **User**: A person who has logged in via Telegram. Identified by a server-assigned record id (the internal handle) and a Telegram user id (immutable, from the login). Carries display information (first name, last name, username, photo URL), an active flag, a last-login timestamp, and a reference to the user's settings.
- **User Session**: A single sign-in event. Carries the user reference, the issued and expires timestamps, and a revocation timestamp. Sessions are the unit of revocation.
- **User Settings**: One row per user. Carries the user's digest cadence, uncategorized label, and delivery format preference.
- **Telegram Channel (catalog)**: A record for each distinct Telegram channel the system has ever seen. Carries the handle, display name, last-known Telegram channel id, and a current status. The catalog is shared across all users; the watch-list is per-user.
- **User Channel (watch-list)**: A many-to-many join between a user and a Telegram channel, with a reference to the user's effective filter override for that channel. One row per (user, channel) the user has subscribed to.
- **User Filter**: A user's per-channel or default filter. Carries the filter type, the filter value, and an active flag. The same user may have at most one default filter and at most one filter per (channel, type).
- **Source Message**: A message observed in a Telegram channel. Carries the source channel reference, the original Telegram message id, the captured text, the media kind, a permalink, a dedup key, and a captured-at timestamp. Shared across all subscribers; lifecycle is per-user below.
- **Cycle**: One execution of the periodic fetch-and-deliver loop. Carries the window start/end, status, input count, output count, and a reference to the per-user delivery records produced in this cycle.
- **User Delivery**: One row per (user, source message, cycle) recording the outcome: sent, filtered out, no match, or send failed. Carries the per-user summary (when sent), the matched filter id, the Telegram message id (when sent), and the failure reason (when failed). This is the per-user delivery history the user sees in their dashboard.
- **Operational Event**: A short audit-log entry for system-level events (login, channel added, cycle completed, delivery failure, etc.) used by the operator dashboard.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new visitor can complete the Telegram login flow and reach an empty, personalized dashboard in under 30 seconds, measured from clicking the login button.
- **SC-002**: A logged-in user can add a public Telegram channel, see it in their list, and remove it, in under 1 minute per action, with the change reflected in their list within 1 second of the API response.
- **SC-003**: A logged-in user can configure a keyword filter on a subscribed channel and observe that subsequent matching messages are delivered to their Telegram chat and non-matching messages are not, in 100% of attempts over a rolling 24-hour window.
- **SC-004**: A logged-in user can configure a natural-language filter on a subscribed channel and observe that the AI correctly identifies matching messages (per a test set of 20 messages with known expected outcomes) in at least 80% of cases, with the system's correct-match rate and false-positive rate reported in the dashboard.
- **SC-005**: When two users are subscribed to the same channel, a single new source message produces one Telegram message per user, never zero, never two for the same user, verifiable by inspecting each user's delivery history.
- **SC-006**: A logged-in user with no filters set on any channel receives every new source message from each subscribed channel in their Telegram chat within one delivery cycle of the message being posted, in 100% of attempts.
- **SC-007**: When the AI service is unavailable, a logged-in user with a natural-language filter set still receives a sensible "best effort" delivery (matching messages with a degraded summary, or a clear "AI unavailable, will retry" placeholder) within one cycle, in 100% of incidents.
- **SC-008**: When the service's bot is blocked by a user, the system records the failure in that user's history, surfaces the issue in their dashboard, and does not retry that user within the same cycle, verifiable by inspecting the user's history.
- **SC-009**: The service supports 1,000 logged-in users subscribed to a single chatty channel (1 message per minute) without dropping any delivery, verifiable by an end-to-end load test.
- **SC-010**: All per-user state (watch-list, filters, settings, delivery history) survives a service restart in 100% of restarts, verifiable by logging out and back in after a restart.
- **SC-011**: A user can change their digest cadence and observe the new cadence take effect on the next cycle, in 100% of attempts.
- **SC-012**: The dashboard's main pages (channel list, filter editor, delivery history, settings) render in under 2 seconds on a typical broadband connection, verifiable by synthetic monitoring.
- **SC-013**: No user can read or modify another user's watch-list, filters, settings, or delivery history, verifiable by a security test that authenticates as user A and attempts cross-user access to user B's resources.
- **SC-014**: The system can be operated by a single operator managing up to 1,000 active users on a single small-to-medium server (one with roughly 2 CPU cores and 256 MB of memory available to the service) without dropping any delivery, verifiable by a 24-hour load test.

## Assumptions

- **Operator-managed bot**: The service runs a single Telegram bot identity (one bot token) for both reading subscribed channels and delivering to users' private chats. Per-user bot identities are out of scope.
- **Auth surface**: The dashboard is a normal web app opened in a browser. The Telegram-hosted web sign-in flow (Login Widget) is the primary auth surface; Mini App identity verification is supported for parity but not the primary path.
- **Existing storage layer**: The service continues to use its current single-file embedded database. The new per-user tables are added to the same database via a forward-only migration. (The planning phase will pin the concrete engine.)
- **Existing channel- and post-fetch infrastructure**: The existing fetch loop (long-poll `getUpdates` or public-web preview) is kept. Per-user fan-out happens after the fetch step and uses the existing post-queue as the unit of work.
- **Existing AI provider**: The existing AI summarizer (and its current provider configuration) is kept. The per-user filter call uses the same provider through a new entry point that takes the user's prompt as additional context.
- **Hard cut from the single-admin model**: The prior `ADMIN_PASSWORD` login and the singleton `settings` row's `telegram_subscriber_chat` field are removed in this refactor. There is no v1 coexistence.
- **Public channels only at launch**: Private channels that the bot has not been added to are out of scope. A user subscribing to a private channel is responsible for adding the bot themselves; the service does not manage that dance.
- **Filter model**: The filter types are limited to keywords, natural-language prompt, and single category. Compound filters (e.g. keywords AND category) are out of scope for v1.
- **Delivery failure semantics**: A blocked-by-user is terminal for that user until they unblock; the system does not auto-retry.
- **Session lifetime**: 12 hours is the default. The operator can configure this in a future iteration.
- **No mobile app**: The dashboard is web-only. Native mobile clients are out of scope.
- **No data export in v1**: Users can view their delivery history in the dashboard. Bulk export and deletion endpoints are out of scope for v1.
- **Bot token is operator-supplied**: The Telegram bot token is supplied by the operator at deploy time, not by individual users. The Telegram-hosted sign-in flow verification uses the same bot token.
- **English-language interface**: The dashboard, error messages, and operator-facing strings are English-only. Localization is out of scope.
- **Compliance**: Each user is responsible for ensuring that the channels they subscribe to and the messages they receive comply with Telegram's Terms of Service and applicable law. The service does not perform legal review of subscribed channels.
