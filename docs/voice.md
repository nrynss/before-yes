# Interface voice

These rules and examples reflect the user’s accepted interface copy.

- Explain what the tool does in concrete terms. “A second look for parents” becomes “A research tool for parents”.
- Keep the user’s chosen headline: “Research first. Yes next.”
- Use supporting text to explain the input and the result: “Enter an app, game or YouTube channel and your child’s age. See what the sources say about chat, purchases, privacy and more.”
- Remove repeated introductions around a form. Keep “What are they asking for?”; remove “The next question”, “A little context. An answer for their age.” and “The same six questions. Sources you can follow.”
- Name the action on the button. “Check before yes” becomes “Check this title”.
- Use an en dash in the home page title: “Before Yes – A research tool for parents”.
- Explain the process with concrete actions: “Find sources”, “Check the evidence” and “Get an age-specific answer”. Use “How a check works” as the heading; remove “From question to clarity.”
- The user requested one theme button with sun, moon and system states. It cycles System → Light → Dark → System and names the current mode and next action accessibly.
- Label the examples “Previous checks”; omit “Free to open” because the tool is currently free.
- Keep the theme button compact and icon-only, with System as the default. Mode names belong in its accessible label, not visible text.
- Use “What we check” and parent-facing area labels: “Contact with strangers”, “Purchases and spending”, “Privacy and data”, “Age restrictions”, “Content” and “Developer history”.
- Keep the footer to “Every concern links to its source.” Remove “For the questions that come before yes.”
- Passcode dialog: “Enter the passcode” and “This demo requires a passcode to start a check.” Omit the duplicate notice. A rejected passcode says “That passcode wasn’t accepted. Try again.”
- Visitor-key dialog: “Use your own API keys”. Explain the check limit, that usage is charged to the visitor’s accounts, and that keys aren’t stored. Omit the repeated yellow notice inside the dialog.
- Progress: “Checking this title” with an animated ellipsis (static under reduced motion); “Your result will open here when the check is complete.” Remove the research eyebrow and slogan. Reuse the home step names. Count “sections read”, not sources.
- Progress title: “Before Yes - Check in progress for <title>” when reached from a submission; omit the title suffix on a direct link without title context. Footnote: “This check keeps running if you leave. Return to this page to see the result.” Let it use the reading column width and wrap naturally on narrow screens.
- Result cards use the home checklist’s area labels and “Concern” for a flagged area. Lead evidence is headed “Why this is a concern”; collapsed citations say “Read the source quotes”. Remove “Before you say yes”, “The same six questions” and “Another possibility”. Card titles follow “Before Yes - <title> - <verdict>”.
- Alternatives are “Other titles to consider”; actions are “View check” and “Compare titles”. A daily-limit notice says “More alternatives can be checked when the daily limit resets.” Keep generated reasons and source quotes unchanged.
- “About this check” uses plain labels for source sections, excluded findings, missing source types, AI models and processing costs. Counts and model identifiers stay as recorded. Sharing uses “Copy card link” and “Card link”. Small print: “This guidance reflects the sources available when the check was run.”

- “About this check” also lists the rule checks, under “Rule checks”, one line for each: the area label, what happened (“Changed from Concern to Not enough evidence.” or “Stays Concern.”), then “Checked by rule:” and the rule’s finding in plain words, for example “Checked by rule: the App Store lists this app as free with no in-app purchases, and no checked quote reports purchases or ads.” These sentences are fixed by the rules, not written by a model.

- Settings: “Settings to change” and “Needs a setting change”. Explain: “Change these settings on your child’s device, then tick them here. Ticking a box doesn’t change the device’s settings.” Remove “A practical checklist”. Keep “OK” and “Not enough evidence”.
- An unknown area without a model explanation says: “We don’t have enough evidence to assess this area. Don’t assume it is safe.” Preserve any model explanation that exists.
- Comparison: “Compare titles” and “Compare the evidence for each title. Check the age shown above each result.” Remove “Side by side”. Links say “View <title>’s check”; tab title is “Before Yes - <title> and <title>”.
- The user also requested matching comparison block sizes: both cells in each row stretch to the taller content, with text aligned at the top.
- Show the comparison introduction as two separate lines, one sentence per line; allow natural wrapping on narrow screens.
- Research with no usable sources: “Sorry, we couldn’t research this title.” then “Check the spelling or try its full name.” Missing check: “Sorry, we couldn’t find this check.” then “Check the link or return home to start again.” Present the situation and next action as separate lines.
- Remaining errors: describe limits, paused checks, busy service and excessive requests plainly; include the actual retry delay when provided. Refer to “previous checks”. Use “We couldn’t reconnect.” / “Try again to see the latest progress.” with a “Reconnect” button. Empty list: “No checks yet. Enter a title above to get started.” Loading: “Loading previous checks…”.


## Separate model-writing follow-up

The user agreed model-generated explanations need a separate task with evaluation, outside T5.9. Teach the model to use parent-facing language, attribute what a source reports, explain uncertainty, avoid internal terms such as “finding” or “mitigation”, and avoid inferences the evidence does not establish. Evaluate varied examples while retaining quote and evidence checks; clear prose alone is not evidence. Do not hard-code replacement reasons for the example cards.
