# Feature Specification: Telegram News Digest Assistant

**Feature Branch**: `001-telegram-news-assistant`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "We need a service that acts as a user assistant. In the first phase of this service, we want to fetch news from specific Telegram channels selected by the user, summarize and categorize the content, and then send it to the user (via a designated Telegram bot) every 10 minutes. The backend of this service will be developed using Go (Golang), and the frontend (admin panel) will be built with Svelte. Artificial Intelligence (AI) should be utilized for summarizing the news."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Receive Periodic News Digest (Priority: P1)

A subscriber receives, on a fixed 10-minute cadence, a single Telegram message from a designated bot that contains summarized and categorized news drawn from the Telegram channels they have selected.

**Why this priority**: This is the core value proposition of the assistant — without an end-to-end digest, the service delivers nothing. Every other capability exists to make this loop reliable, configurable, or richer.

**Independent Test**: With at least one channel selected and at least one message posted in that channel within the current 10-minute window, the subscriber receives a Telegram message from the designated bot within 10 minutes (plus a small processing margin) of the window closing, containing a summary of the message and a category label.

**Acceptance Scenarios**:

1. **Given** the subscriber has selected one or more Telegram channels and at least one new message exists in those channels during the last 10 minutes, **When** the scheduled digest window closes, **Then** the designated Telegram bot delivers a single message to the subscriber containing a summary of each new message, grouped by category.
2. **Given** no new messages have appeared in any selected channel during the last 10 minutes, **When** the scheduled digest window closes, **Then** the system does not send a notification message (or sends an explicit "no new items" message) so the subscriber is not spammed.
3. **Given** the subscriber has selected five channels and a total of 30 new messages exist across them, **When** the digest is delivered, **Then** each message is summarized (not just linked or quoted) and the summaries are grouped under category headings inside one Telegram message.
4. **Given** the AI summarization service is temporarily unavailable, **When** the digest window closes, **Then** the system retries or falls back gracefully so the subscriber still receives a digest (e.g., a degraded digest with raw headlines) within an extended window rather than missing the cycle.

---

### User Story 2 - Select and Manage Source Channels (Priority: P1)

The subscriber uses an admin panel to add, review, and remove the Telegram channels the assistant will monitor on their behalf.

**Why this priority**: The digest content is entirely determined by which channels are selected. Without an easy way to curate channels, the subscriber cannot shape what they receive.

**Independent Test**: Open the admin panel, add a channel by its public Telegram handle, confirm it appears in the selected-channel list, and verify that within one digest cycle the new channel's content (if any) is included in the next delivered digest.

**Acceptance Scenarios**:

1. **Given** the subscriber is viewing the admin panel, **When** they enter a valid public Telegram channel handle and confirm, **Then** the channel appears in their selected-channel list and is monitored from the next digest cycle onward.
2. **Given** the subscriber has one or more channels selected, **When** they remove a channel from the list, **Then** that channel is excluded from the next digest cycle and the change is persisted across service restarts.
3. **Given** the subscriber enters an invalid or non-existent Telegram handle, **When** they confirm, **Then** the system rejects the entry with a clear explanation and does not store it.
4. **Given** the subscriber is using the admin panel, **When** they view the channel list, **Then** each entry shows the channel name, handle, and the last time a new message was observed there.

---

### User Story 3 - View and Configure Categories (Priority: P2)

The subscriber can see which categories the assistant uses to group news and can adjust the category list (add, rename, remove) to match the topics they care about.

**Why this priority**: Categorization is explicitly part of the requested behavior. Letting the subscriber refine categories turns a fixed taxonomy into a personalized one, which is the meaningful differentiator over a plain summary feed.

**Independent Test**: Open the category settings in the admin panel, add a new category, remove an unused one, then observe that the next delivered digest groups summaries under the updated set of categories.

**Acceptance Scenarios**:

1. **Given** the assistant ships with a default set of categories, **When** the subscriber first uses the service, **Then** summaries in the delivered digests are grouped under those default categories.
2. **Given** the subscriber adds a new category (for example, "AI & ML"), **When** the next digest is prepared, **Then** incoming messages classified into the new category are grouped under that heading.
3. **Given** the subscriber removes a category, **When** the next digest is prepared, **Then** no summaries are grouped under that heading; affected items are either reassigned to a default category or marked as "Uncategorized" in a visible way.

---

### User Story 4 - Operate the Service via Admin Panel (Priority: P2)

An operator uses the admin panel to configure the service: register or rotate the designated Telegram bot token, connect the AI provider credentials, set the digest interval (default 10 minutes), and observe operational health.

**Why this priority**: Even for a single-subscriber service, the operator needs a way to manage credentials, change the interval, and diagnose issues without editing files or restarting the system by hand.

**Independent Test**: Open the operator section of the admin panel, change the digest interval from 10 minutes to a different value, save, and verify the next digest is delivered on the new cadence (and that the change persists across restarts).

**Acceptance Scenarios**:

1. **Given** the operator opens the admin panel, **When** they view the settings page, **Then** they can see and update the current digest interval, the active Telegram bot token status, the AI provider status, and the last successful digest timestamp.
2. **Given** the operator updates the digest interval, **When** they save, **Then** the scheduling system adopts the new interval from the next cycle onward and the change is persisted.
3. **Given** a credential (Telegram bot token or AI provider key) is invalid or expired, **When** the operator views the settings page, **Then** the system shows a clear health/error indicator and the most recent failure reason.

---

### User Story 5 - Observe Digest History and Audit Trail (Priority: P3)

The subscriber (and operator) can review past delivered digests and the underlying source messages that produced them, so they can revisit, audit, or recover summaries they may have missed.

**Why this priority**: This is a quality-of-life capability. It is not required for the core loop, but it gives the service credibility (no lost digests) and supports debugging.

**Independent Test**: Generate a few digests, then open the history view in the admin panel and confirm each digest is listed with timestamp, source channels, number of messages, and clickable summary content.

**Acceptance Scenarios**:

1. **Given** at least one digest has been delivered, **When** the subscriber opens the history view, **Then** each past digest is listed in reverse chronological order with its delivery timestamp and a count of items summarized.
2. **Given** a digest is listed in the history, **When** the subscriber opens it, **Then** the full categorized summary content is visible along with the source channel handles that contributed to it.

---

### Edge Cases

- **Bursty channels**: A single selected channel posts hundreds of messages inside one 10-minute window. The system must not let one channel dominate or blow past Telegram's per-message length limit; the digest must remain a single (or bounded) Telegram message(s) and noisy channels must be handled gracefully (e.g., top-N most relevant, or grouped "N similar items" with a sample).
- **Duplicate messages**: A message is forwarded into multiple selected channels. The system must deduplicate so the same source content does not appear twice in one digest.
- **Deleted / edited messages**: A source message is deleted or edited in Telegram after the digest window opens. The digest must use the version captured at fetch time and must not break if the source becomes inaccessible later.
- **Non-text content**: A channel post is media-only (image, video, voice) with little or no caption. The system must produce a useful summary (caption, transcript if available, or an explicit "non-text item" marker) rather than dropping the item silently.
- **Bot blocked by subscriber**: The subscriber blocks the Telegram bot. The next delivery fails. The system must record the failure, surface it in the admin panel, and back off rather than retrying indefinitely.
- **Telegram rate limits**: The system falls behind while fetching or sending. The system must respect Telegram's rate limits, queue work, and not lose items; if a cycle slips, the next cycle must catch up without re-summarizing items already delivered.
- **AI provider outage**: The summarization service is unavailable. The system must fall back to a degraded digest (e.g., raw headlines + categories) and surface the degraded state, rather than skipping the cycle entirely.
- **Clock drift / scheduler restart**: The service restarts mid-window. The system must not double-deliver the same window and must not skip a window on restart.
- **Channel privacy / access**: A selected channel becomes private or bans the bot. The system must detect this, surface it in the admin panel, and exclude the channel from future digests without crashing.
- **Empty category set**: The subscriber removes all categories. The system must still deliver a digest (grouped as "Uncategorized" or top-level) rather than failing.
- **Configuration changes mid-cycle**: The operator changes the interval or the channel list while a digest is being prepared. The system must use a consistent snapshot for the current cycle and apply the change from the next cycle onward.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The service MUST allow the subscriber to select a set of public Telegram channels (by handle) that the assistant will monitor for new messages.
- **FR-002**: The service MUST persist the subscriber's selected-channel list across service restarts.
- **FR-003**: The service MUST continuously (or on the digest tick) fetch new messages from each selected Telegram channel that have appeared since the last successful digest.
- **FR-004**: The service MUST produce a digest on a fixed cadence. The default cadence is every 10 minutes; the cadence MUST be configurable.
- **FR-005**: The service MUST use an AI component to generate a short summary for each fetched message.
- **FR-006**: The service MUST assign each fetched message to one category from a configured category set (a default set is provided and is subscriber-editable).
- **FR-007**: The service MUST deliver a single Telegram message (or a tightly bounded set of messages) from a designated bot to the subscriber, containing all summaries grouped by category, on every cycle where there is at least one new item.
- **FR-008**: The service MUST NOT deliver a notification to the subscriber for cycles where no new items exist in any selected channel.
- **FR-009**: The service MUST deduplicate items that appear in more than one selected channel within the same cycle.
- **FR-010**: The service MUST respect Telegram's message size limits and channel content constraints; if a digest would exceed limits, the system MUST truncate or split in a deterministic, documented way (e.g., top-N per category with a "more" marker).
- **FR-011**: The service MUST provide an admin panel where the subscriber can view, add, and remove selected channels; view, add, rename, and remove categories; and view digest history.
- **FR-012**: The service MUST provide an operator section in the admin panel where an operator can configure the digest interval, register/rotate the Telegram bot token, configure AI provider credentials, and view operational health (last successful digest, last failure, per-channel status).
- **FR-013**: The service MUST persist all configuration (channels, categories, interval, credentials references) so the system can restart and resume on the same configuration.
- **FR-014**: The service MUST record every delivered digest, its timestamp, the source channels that contributed, and the rendered summary, so it can be reviewed in the history view.
- **FR-015**: The service MUST surface failures (Telegram API errors, AI provider errors, scheduling slips, blocked-bot) in the admin panel with a human-readable reason and a timestamp.
- **FR-016**: The service MUST NOT double-deliver a digest window after a restart, and MUST NOT skip a window on restart, even when the restart happens mid-window.
- **FR-017**: The service MUST handle non-text source messages (images, video, voice) by producing a useful entry in the digest (caption, transcript when available, or an explicit "non-text item" marker) rather than silently dropping them.
- **FR-018**: The service MUST treat the AI provider as a pluggable component so that the underlying model/vendor can be changed without changing the rest of the system. The exact provider is not fixed by this spec.

### Key Entities *(include if feature involves data)*

- **Subscriber**: The single person who receives digests and selects channels/categories. Attributes: identity, Telegram chat identifier, selected channels, custom categories (overlays on defaults), digest interval preference.
- **Source Channel**: A public Telegram channel the subscriber wants monitored. Attributes: handle, display name, last-seen message identifier, last-observed timestamp, status (active / inaccessible / banned).
- **Source Message**: A message observed in a source channel during a digest window. Attributes: channel reference, Telegram message identifier, captured text/media-reference, captured timestamp, dedup key, raw payload snapshot (the version used for summarization).
- **Digest Item**: The summarized, categorized unit that goes into a digest. Attributes: source-message reference, summary text, assigned category, confidence (if available), order within its category.
- **Category**: A label used to group digest items. Attributes: name, ordering weight, default-or-custom flag. The service ships with a default set; the subscriber can add, rename, and remove.
- **Digest Cycle**: One scheduled execution of the digest loop. Attributes: cycle identifier, window start/end timestamps, status (pending / succeeded / failed / degraded), input message count, output digest reference.
- **Digest Record**: The delivered artifact for one cycle. Attributes: cycle reference, rendered Telegram message text, delivery timestamp, delivery status, per-category contents.
- **Operator Configuration**: Settings managed by the operator. Attributes: digest interval, Telegram bot token reference, AI provider credentials reference, feature flags.
- **Operational Health Snapshot**: Read-only diagnostic state. Attributes: last successful cycle timestamp, last failure timestamp and reason, per-channel fetch status, AI provider status, Telegram bot reachability status.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A subscriber with at least one selected channel that receives new messages sees a categorized digest delivered to their Telegram chat within 10 minutes (plus a small processing margin of up to 60 seconds) of those messages being posted, measured end-to-end.
- **SC-002**: Cycles in which no selected channel produced new content produce no subscriber-visible message, verified by counting delivered digests over a 24-hour window with low-activity channels.
- **SC-003**: Each item in a delivered digest is a generated summary, not a verbatim copy of the source message; this is verifiable by comparing lengths and content shape between source and digest for a sample of 100 items.
- **SC-004**: 95% of digest cycles complete successfully (delivered or deliberately suppressed as "no items") over a rolling 7-day window; cycles that fall back to a degraded digest still count as successful as long as the subscriber receives something.
- **SC-005**: The subscriber can add or remove a channel through the admin panel and see the change reflected in the very next delivered digest, in 100% of attempts.
- **SC-006**: The subscriber can add, rename, or remove a category through the admin panel and see the change reflected in the very next delivered digest, in 100% of attempts.
- **SC-007**: When the AI provider is unavailable, the subscriber still receives a digest (degraded mode) within an extended window of at most 25 minutes, in 100% of such incidents.
- **SC-008**: No digest is delivered twice for the same window, even after a service restart, verifiable by inspecting digest history for a forced restart mid-window.
- **SC-009**: The admin panel renders the configuration pages, channel list, category list, and history view for the operator/subscriber in under 2 seconds on a typical broadband connection.
- **SC-010**: All configuration (channels, categories, interval, credential references) survives a full service restart without operator action, in 100% of restarts.
- **SC-011**: The system remains a single (or tightly bounded) Telegram message per cycle for the common case (up to 50 summarized items across categories), verified by inspecting the rendered output.

## Assumptions

- **Audience and scope**: This is the first phase of a broader "user assistant" service. For phase 1 the service supports a single subscriber and a single designated Telegram bot; multi-subscriber support is out of scope for this spec.
- **Channel type**: Selected channels are public Telegram channels. The bot is assumed to have read access to the channels it needs to monitor (joining or being granted access is treated as an operator setup step, not part of the runtime loop).
- **Designated bot identity**: There is one Telegram bot identity used both to read source channels and to deliver the digest to the subscriber. The spec does not require separating the read-side identity from the deliver-side identity.
- **Time window**: A "10-minute window" is measured from the moment a digest cycle starts. The subscriber sees digests at predictable wall-clock times modulo the chosen interval and a small processing delay.
- **AI summarization**: The service uses a third-party AI summarization capability. The provider/model is not fixed by this spec and can be changed by configuration. The service treats the AI as a black box that takes a text (and optional media reference) and returns a summary and a category.
- **Category defaults**: The service ships with a reasonable default category set (e.g., Politics, Technology, Business, Sports, World, Other). The exact default list is a product decision and is not constrained by this spec.
- **Admin panel audience**: The "subscriber" and the "operator" may be the same person for phase 1. The admin panel is a single web application that exposes both subscriber-facing and operator-facing sections; the access boundary between them is enforced in code, not by deploying two separate apps.
- **Credential storage**: Telegram bot tokens and AI provider credentials are stored in the service's secret store, not in plaintext configuration files. The exact secret store is an implementation choice.
- **Non-text messages**: When a source message is image/video/voice with no usable caption, the system falls back to a generic marker (e.g., "[Image]") and notes it in the digest. OCR/ASR are not in scope for phase 1.
- **Digest delivery format**: A digest is rendered as a single Telegram message (or a bounded set of messages if Telegram's size limit is hit). The exact visual style is a product decision and is not constrained by this spec.
- **Failure semantics**: "Degraded mode" means: the subscriber still receives a digest, but summaries may be replaced by raw headlines/captions and an indicator is shown in the message.
- **Deployment topology**: The service is operated as a single logical deployment (backend + admin panel). The deployment surface (containers, hosts, scaling) is not constrained by this spec.
- **Language and locale**: The set of supported source and target languages is a product decision and is not constrained by this spec. The service is expected to operate correctly on whatever the first supported language pair turns out to be.
- **Legal and ToS**: The subscriber is responsible for ensuring that monitoring and summarizing the chosen channels complies with Telegram's Terms of Service and applicable law. The service does not provide legal review of selected channels.
