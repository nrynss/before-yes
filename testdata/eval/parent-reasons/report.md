# Parent-readable reasons evaluation

The calibrated grade prompt, area definitions, citation checks, setting grounding, original entailment prompts and independent age-event classifier are unchanged. A separate fast, metered writer can change only the final reason string. Every proposed factual explanation is checked against the exact cited quotes for support, source attribution and voice. One bounded repair gets the same check. An unknown area makes no title claim; rejected/missing/malformed prose uses a cautious fallback. No wording-only change alters a grade, setting, citation, evidence hash or verdict.

The before measurements use the judge from `67501f0` with the current measurement-only harness. Full collected findings are hashed in canonical JSON order; before/after hashes match. Outside-demo cases are judge-only probes using exact paragraphs verified against a saved comparison-guide capture; they are not new full research checks or independent confirmations of the guide's claims.

## Final comparisons against docs/voice.md

Every row below compares the actual published reason. ‘Specific report’ means a source-attributed statement; ‘fallback’ means the writer/checker did not produce a verified explanation and the card sends the parent to the source quotes. Unknowns use a plain evidence gap and explicitly avoid reassurance. Fallbacks are intentionally disclosed as incomplete specificity, rather than counted as successful generated explanations.

### Frozen fixtures: Khan Academy Kids, age 5

Verdict: **unclear → unclear**. Evidence SHA-256: `97ba24b12f125dc38051f3be22d31962aeeae8a3dcbb9ef92471091a2f4455c0`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | Changed to unknown: the evidence was thin. No verified quote names the setting. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00000000** (reader/gather evidence reused).

### Frozen fixtures: Minecraft, age 9

Verdict: **unclear → unclear**. Evidence SHA-256: `c3f24a88fe1f18ae245f473e5bf7b6f771fc43561e1885c3ddcc89b6a47e5ac8`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | The findings are too thin to say. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | The findings are too thin to say. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00000000** (reader/gather evidence reused).

### Frozen fixtures: Roblox, age 9

Verdict: **no → no**. Evidence SHA-256: `85d83d14229bd9ca2694671989827df650d2cb8675da1de4deba7a47656537fb`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / flag | the title pushes purchases at children | The store page notes that games may contain optional in-game purchases, including lootboxes. | Specific report; source attribution and concrete detail. |
| data / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / flag | the age gate fails in practice | A news article reports that a kid who wants to dodge the age check can simply enter any birthdate before September 2007. | Specific report; source attribution and concrete detail. |
| content / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / flag | this title's developer has a record of enforcement over how it treats children or their data | A news article reports that Los Angeles County filed a civil lawsuit against Roblox Corporation alleging the company falsely marketed its platform as safe for children while knowingly allowing it to become a hunting ground for predators. | Specific report; source attribution and concrete detail. |

Incremental metered judge cost for final prose: **$0.00180000** (reader/gather evidence reused).

Set totals: **3 specific explanations; 0 cautious fallbacks; 15 unknown evidence gaps**. Specificity rate among non-unknown areas: **3/3**.

### Saved gather 1: Khan Academy Kids, age 5

Verdict: **no → no**. Evidence SHA-256: `f8536883e5b76febfdb8ea044925652b40c8c12476eeaab14ca4f63e4f25a723`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / ok | The app is free with no in-app purchases, loot boxes, or subscriptions. | A review reports 'ACTUALLY FREE YOU DONT NEED TO PAY' and describes using the app for school work without payment. | Specific report; source attribution and concrete detail. |
| data / ok | The app is COPPA-compliant and collects no personally identifiable information. | The store listing states the app is 'COPPA-compliant' and created with education experts, though this quote does not verify actual practices. | Specific report; source attribution and concrete detail. |
| age_gating / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / flag | parents or reviewers report children actually meeting unsuitable material on this title | A review reports that 'a grid of scary devil faces and men tied up with their mouth taped shut popped up for about 15 seconds while my kid was working on math.' | Specific report; source attribution and concrete detail. |
| developer_record / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00175320** (reader/gather evidence reused).

### Saved gather 1: Minecraft, age 9

Verdict: **unclear → unclear**. Evidence SHA-256: `d42026185dc69cd24f9fc7d264ca08e1ca187c017c9b6e7bf5c2e858630ff0c0`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00000000** (reader/gather evidence reused).

### Saved gather 1: Roblox, age 9

Verdict: **no → no**. Evidence SHA-256: `e7c67c61bd9cdebb2f7678444aa6f1599ebee26886f15626178947bc4d14db63`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | Changed to unknown: the reason says more than the cited quotes support. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / flag | the age gate fails in practice | A news report states that a child can easily bypass age restrictions by entering any birthdate before September 2007. | Specific report; source attribution and concrete detail. |
| content / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00075840** (reader/gather evidence reused).

Set totals: **4 specific explanations; 0 cautious fallbacks; 14 unknown evidence gaps**. Specificity rate among non-unknown areas: **4/4**.

### Saved gather 2: Khan Academy Kids, age 5

Verdict: **unclear → unclear**. Evidence SHA-256: `a20799400e16b7f69f0d5d7ac89df040ad0b1969c9285c2ec4bceb1e30c1007c`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | Changed to unknown: no verified finding supports ok. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00000000** (reader/gather evidence reused).

### Saved gather 2: Minecraft, age 9

Verdict: **no → no**. Evidence SHA-256: `33482fb5a746f56f3528ed0c91780bf771791321fd9f09e57a95857ea47e6d96`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / flag | the title pushes purchases at children | A reviewer who says they are 10 years old reports buying 10,000 Minecoins using birthday money and feeling scammed. | Specific report; source attribution and concrete detail. |
| data / unknown | Changed to unknown: the cited quotes do not meet this area's flag definition. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | Changed to unknown: the reason says more than the cited quotes support. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | Changed to unknown: the evidence was thin. No verified quote backs the reason the judge gave. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | Changed to unknown: the evidence was thin. No verified quote backs the reason the judge gave. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00078690** (reader/gather evidence reused).

### Saved gather 2: Roblox, age 9

Verdict: **no → no**. Evidence SHA-256: `feb56959d7ade79df1b9715bacd6b46947d3d19dd5c39f81ab8a6e6092fa52b4`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / flag | strangers can reach this child by message, chat or voice, and no control a parent can set shuts that door | A forum post reports that chats built into the coding of specific mini-games cannot be disabled, and kids can type text on signs regardless of communication preferences. | Specific report; source attribution and concrete detail. |
| money / flag | the title pushes purchases at children | A review reports opening the game prompted them to upgrade, requiring adding a payment option in the App Store, which resulted in a $46 charge after entering card details. | Specific report; source attribution and concrete detail. |
| data / flag | children's data is collected and used in ways a parent cannot control | The terms state Roblox may access and review Content, including Content shared via Communication Features and other non-public Content, and a forum post indicates images used for age verification may be processed by third-party providers without a guaranteed retention period for children's images. | Specific report; source attribution and concrete detail. |
| age_gating / flag | the age gate fails in practice | A review reports contacting Roblox support and being told to wait until age 13, while the system displayed an assigned age of 41 despite the user stating they are younger than 13. | Specific report; source attribution and concrete detail. |
| content / flag | parents or reviewers report children actually meeting unsuitable material on this title | A forum post describes that within seconds, the 'Street Code' game showed an intense, fiery car accident and prompted the player to decide whether to shoot or save an officer. | Specific report; source attribution and concrete detail. |
| developer_record / flag | this title's developer has a record of enforcement over how it treats children or their data | A news article states the Texas Attorney General has sued Roblox over 'flagrantly ignoring' safety laws and 'deceiving parents' about the dangers the platform poses to young people. | Specific report; source attribution and concrete detail. |

Incremental metered judge cost for final prose: **$0.00586020** (reader/gather evidence reused).

Set totals: **7 specific explanations; 0 cautious fallbacks; 11 unknown evidence gaps**. Specificity rate among non-unknown areas: **7/7**.

### Outside demo: judge-only: LEGO Worlds, age 9

Verdict: **unclear → unclear**. Evidence SHA-256: `35556ae247099f92d0d3ec3b2e78c2b56bd5aed98787bcc5c0ea4eff133c7620`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / ok | The comparison guide states there is no open chat with strangers. | The comparison guide reports there is no open chat with strangers. | Specific report; source attribution and concrete detail. |
| money / unknown | No findings were provided about in-app purchases, monetization, or financial risks. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| data / unknown | No findings were provided about data collection, tracking, or privacy practices. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | No findings were provided about age verification or age-gating effectiveness. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | No findings were provided about exposure to unsuitable content such as violence, horror, or harassment. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | No findings were provided about the developer's history of fines, breaches, lawsuits, or regulatory actions. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00073650** (reader/gather evidence reused).

### Outside demo: judge-only: Scratch, age 9

Verdict: **unclear → unclear**. Evidence SHA-256: `dd2bb91632b3dc85039a4efd517378655dcd379b266d085b82bbc757e482e5d7`. Grade/settings/citation changes: **0**.

| Area / grade | Before | After | Voice assessment |
|---|---|---|---|
| contact / unknown | No findings were provided about whether strangers can message or voice chat with the child, or about controls to shut that door. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| money / ok | The description emphasizes Scratch as a free, educational coding platform with no mention of in-app purchases, loot boxes, or financial pressure on children. | The comparison guide calls Scratch MIT's free, block-based coding platform. | Specific report; source attribution and concrete detail. |
| data / unknown | No findings were provided about data collection, tracking, third-party sharing, or deletion controls. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| age_gating / unknown | No findings were provided about whether the age gate works in practice or if children under the stated age can access the platform. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| content / unknown | No findings were provided about whether children encounter unsuitable material such as sexual content, violence, horror, harassment, or self-harm. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |
| developer_record / unknown | No findings were provided about the developer's history of fines, breaches, lawsuits, or regulatory actions regarding children or their data. | We don’t have enough evidence to assess this area. Don’t assume it is safe. | Plain evidence gap; no factual title claim; no safety inference. |

Incremental metered judge cost for final prose: **$0.00159900** (reader/gather evidence reused).

Set totals: **2 specific explanations; 0 cautious fallbacks; 10 unknown evidence gaps**. Specificity rate among non-unknown areas: **2/2**.

## Cost and verdict accounting

| Evidence set | Title | Before verdict | After verdict | Final prose cost increment |
|---|---|---|---|---|
| Frozen fixtures | Khan Academy Kids | unclear | unclear | $0.00000000 |
| Frozen fixtures | Minecraft | unclear | unclear | $0.00000000 |
| Frozen fixtures | Roblox | no | no | $0.00180000 |
| Saved gather 1 | Khan Academy Kids | no | no | $0.00175320 |
| Saved gather 1 | Minecraft | unclear | unclear | $0.00000000 |
| Saved gather 1 | Roblox | no | no | $0.00075840 |
| Saved gather 2 | Khan Academy Kids | unclear | unclear | $0.00000000 |
| Saved gather 2 | Minecraft | no | no | $0.00078690 |
| Saved gather 2 | Roblox | no | no | $0.00586020 |
| Outside demo: judge-only | LEGO Worlds | unclear | unclear | $0.00073650 |
| Outside demo: judge-only | Scratch | unclear | unclear | $0.00159900 |

Whole-task measured spend: **$0.24024090 / $0.50**. This includes rejected grading-prompt pilots, superseded prose attempts, repairs, outside-case baselines and the final pass. Final prose alone adds **$0.01329420** across the eleven cases; the rest is retained task experimentation, not hidden from the cap.

Final grade/verdict delta count: **0**. Final evidence, settings and citation delta count: **0**. The replay regression test checks these identities over the complete recordings, including both outside-demo cases.

## Rejected grading-prompt approach

Adding voice instructions directly to the grade prompt changed citations and semantic judgments. That approach was rejected; `failed-pilot.patch` records it, and `pilot/fixture.json` / `pilot/gather-1.json` retain the measured outputs. Those model replies remain in the recording store and their spend remains on the shared ledger. The final design reuses the original grading recordings and never asks the prose writer to regrade.

| Set | Title | Area | Before | Rejected pilot | Explanation |
|---|---|---|---|---|---|
| fixture | Khan Academy Kids | contact | unknown | ok | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Reassurance did not establish the claimed absence or effectiveness. |
| fixture | Khan Academy Kids | money | unknown | ok | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Reassurance did not establish the claimed absence or effectiveness. |
| fixture | Khan Academy Kids | age_gating | unknown | ok | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Reassurance did not establish the claimed absence or effectiveness. |
| fixture | Khan Academy Kids | content | unknown | ok | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Reassurance did not establish the claimed absence or effectiveness. |
| fixture | Khan Academy Kids | developer_record | unknown | ok | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Reassurance did not establish the claimed absence or effectiveness. |
| fixture | Khan Academy Kids | VERDICT | unclear | yes | Follows the rejected area changes above; absent in the final isolated prose pass. |
| fixture | Minecraft | content | unknown | flag | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. It treated a review's disliked religious imagery as horror; rejected as unearned. |
| fixture | Minecraft | VERDICT | unclear | no | Follows the rejected area changes above; absent in the final isolated prose pass. |
| fixture | Roblox | money | flag | unknown | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Changed citation lists failed the original checks. |
| fixture | Roblox | age_gating | flag | unknown | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Changed citation lists failed the original checks. |
| fixture | Roblox | developer_record | flag | unknown | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Changed citation lists failed the original checks. |
| fixture | Roblox | VERDICT | no | unclear | Follows the rejected area changes above; absent in the final isolated prose pass. |
| gather-1 | Khan Academy Kids | money | ok | unknown | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. |
| gather-1 | Roblox | contact | unknown | flag | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. |
| gather-1 | Roblox | age_gating | flag | unknown | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. Changed citation lists failed the original checks. |
| gather-1 | Roblox | content | unknown | flag | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. |
| gather-1 | Roblox | developer_record | unknown | flag | Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged. |

## Quote witnesses for every non-unknown area

The complete citation sets remain in each diagnosis. These verbatim witnesses make the reason assessment inspectable; they are not replacement explanations. The age-gating witness below is the independently verified event, which can differ from the source report selected by the prose writer.

| Set | Title / area | Source kind | Quoted witness |
|---|---|---|---|
| 0 | Roblox / money | store | Games may contain optional in-game purchases, including lootboxes and other items |
| 0 | Roblox / age_gating | news | a kid who wants to dodge all of the above can simply enter any birthdate before September 2007. |
| 0 | Roblox / developer_record | news | Los Angeles County filed a civil lawsuit against Roblox Corporation alleging the company falsely marketed its online gaming platform as safe for children while knowingly allowing it to become a “hunting ground for predators.” |
| 1 | Khan Academy Kids / money | review | ACTUALLY FREE YOU DONT NEED TO PAY Thank goodness I use khan academy for school work so when I found out they had a learning games for kids I was so happy and I thought “ they teach fee videos on YouTube “ and it absolutely was free glad to know someone cares for our youth |
| 1 | Khan Academy Kids / data | store | Created in partnership with the Stanford Graduate School of Education by education experts, aligned with Head Start and Common Core Standards, COPPA-compliant, and 100% free—no ads, no subscriptions. |
| 1 | Khan Academy Kids / content | review | A grid of scary devil faces and men tied up with their mouth taped shut popped up for about 15 seconds while my kid was working on math. Very disturbing and disappointing since this is such a widely advertised app for homeschooling |
| 1 | Roblox / age_gating | news | A kid who wants to dodge all of the above can simply enter any birthdate before September 2007. |
| 2 | Minecraft / money | review | i bought 10,000 minecoins and im litterly a kid im only 10 and i got scammed i litterly used all my birthday money! FRICK YOU MINECRAFT |
| 2 | Roblox / contact | forum | Chats built into the coding of specific mini-games cannot be disabled, and kids can type text on "signs" regardless of communication preferences. |
| 2 | Roblox / money | review | I don’t know if anyone else has been having the same problem but it will not let me play without paying. When I open up the game it tells me to upgrade it, when I try to upgrade it in the App Store it tells me e that I need to add a payment option. So I go to add my card info, thankfully I added the one with no money, and I looked to see that I got a $46 charge. This is unacceptable when you guys advertise the game as free. |
| 2 | Roblox / data | forum | Images used for age verification may be processed by third-party providers, but Roblox does not publicly guarantee a fixed retention period for the images of children. |
| 2 | Roblox / age_gating | review | the pencil icon nor the link parent icon was there so if you could add a feature to ask Roblox themselves to reset your Face ID scan that would be nice and I contacted Roblox support and they said I gotta wait until I’m 13 but the birthday said Oct 7,1985 and I didn’t know how that worked… because it said that I’m 41 but I’m younger than 13 |
| 2 | Roblox / content | forum | Within seconds, this "Street Code" game showed an intense, fiery car accident and prompted the player to decide whether to shoot or save an officer. |
| 2 | Roblox / developer_record | news | Texas Attorney General Ken Paxton says he has sued Roblox over "flagrantly ignoring" safety laws and "deceiving parents" about the dangers the online video gaming platform poses to young people. |
| outside | LEGO Worlds / contact | comparison guide | LEGO Worlds gives you the creative freedom of a sandbox inside a calmer, more structured, LEGO-branded package. There's no open chat with strangers, it carries a family-friendly rating, and the toy-inspired building will feel instantly familiar to any kid who's snapped bricks together. If you want the joy of construction without the open social layer, it's one of the easiest recommendations on this list. |
| outside | Scratch / money | comparison guide | If what your child loves about Roblox is the *making* — designing their own games and worlds — Scratch is the real thing. It's MIT's free, block-based coding platform, and kids genuinely learn programming concepts like loops and variables while building. The community is moderated, there's a huge library of projects to remix, and it all runs in the browser. |

## Evidence and limits

Saved gather diagnoses next to each capture show every final grade, original entailment result, age witness and complete cited quotes. Fixture diagnoses are reproducible with `-diagnose`; no saved title fixture is edited. The machine-readable before/after snapshots include the full grades and evidence hash, not merely the computed verdict.

The final prose checks are model judgments, not deterministic semantic proofs. A rejected explanation never changes the calibrated grade. The cautious fallback avoids introducing a factual claim but is less specific; the rates above make that limitation visible. Unknown areas remain unknown. A compliance label is described only as the source’s claim with an explicit limit; it does not establish actual privacy compliance.

Regression pins retain Minecraft fixture gameplay contact unknown, Minecraft gather-1 generic tracking unknown, Minecraft gather-2 child-account-only age mitigation unknown, and Khan gather-2 curriculum age unknown. Both live Roblox age flags retain the original independent event witness; the conditional-correction ablation and source-witness unit tests remain. The age writer receives the already validated source event as its focus; it cannot replace or alter that witness. Diagnostic causes are captured before public prose replaces the internal check explanations. Data, money and content prose is checked independently without the old copied-definition allowance; rejection leaves the earned grade and a cautious explanation.

## Reproduce without a key

```sh
for n in 0 1 2; do
  go run ./tools/eval -models replay -gather "$n" -json
done
go run ./tools/eval -models replay -reason-cases testdata/eval/parent-reasons/outside-cases.json -json
python3 tools/eval/parent_reason_report.py
```

The live recording command uses `-models live -reuse-gather -live-cap-usd 0.50 -live-ledger /tmp/before-yes-t3.14-live-ledger.json`; the same ledger covered every pilot, repair and outside-case baseline. Do not reset it to re-run the task.


## Round 1 remediation evidence

The review remains `REMEDIATE` as an independent historical verdict. This change addresses M1, M2, M3, L1 and L2 for a fresh review; it does not supply user sample signoff or close the task. No push or merge.

### Design and rejected attempts

The calibrated grade prompt, definitions, original entailment prompts, citation and setting checks, and independent age-event classifier retain their decisions. The prose pass verifies each published claim and its relevance to the fixed area, rather than asking whether the source earns the fixed grade again. It receives an explanation focus, never an exemption based on a grade. Exact factual support, attribution, age comparisons, causation and encounter relevance remain required. The age writer receives the already validated source-event witness captured from the original check; it makes no extra classifier request and cannot replace that witness.

The first remediation attempts were rejected rather than treated as successful examples. Their complete results are in `remediation-attempts/`, and all raw writer/checker replies remain under their original request hashes. Checker explanations now identify the rejected claim and feed the single bounded repair. Repeated overconservative checks demanded facts the prose did not claim: parental oversight or a purchase method for an attributed spending report, restricted-content access for an explicit reported age mismatch, or proof of no deletion for a quoted third-party-processing/retention report. Other incorrect checks demanded an unknown forum speaker identity or treated a reviewer's quoted self-reported age as a claim about this check's child. The narrowed writing/checking focus addresses those failures without permitting extra pressure, permission, advertising, causes or ages. Later attempts exposed overly broad source relevance; the final focus requires the actual area evidence, and the published data explanation includes the third-party/retention report.

| Finding | Change | Reproducible pin |
|---|---|---|
| M1 | The writer receives the already validated age event. The checker rejects wrong-group comparisons and causes inferred from ambiguous group numbers. Final gather-2 Roblox prose reports the source's actual/assigned-age mismatch, without a claimed cause. | `go test ./internal/pipeline -run 'TestParentReasonRejectsRecordedUnsupportedProse/age_wrong_group_cause\|TestParentWriterUsesValidatedAgeEventFocus' -count=1`; replay gather 2, inspect age reason and exact `younger than 13` / `41` witness in its diagnosis. |
| M2 | All five prior fallback areas now describe supported title-specific evidence. Runtime fallback remains for failed/missing/malformed candidates or checks. Qualified optional purchases remain qualified; self-reported spending stays attributed; compliance is only the store's claim, with an explicit verification limit; third-party image processing/retention remains the quoted report. | `go test ./tools/eval -run TestPublishedParentReasonsRegression -count=1`; all sixteen non-unknown areas have accepted specific reasons, and every original non-prose field is pinned by `TestParentReasonsPreserveRecordedGradesAndEvidence`. |
| M3 | Content prose selects the reported Street Code encounter (fiery crash and shoot/save prompt), rather than generalized possible/likely content or a game's description alone. | `go test ./internal/pipeline -run TestParentReasonRejectsRecordedUnsupportedProse/content_possibility -count=1`; `go test ./tools/eval -run TestPublishedParentReasonsRegression -count=1`; gather-2 Roblox diagnosis includes exact encounter quote `f_6fab8dae3bd61975`. |
| L1 | Scratch's positive source description is concise: “The comparison guide reports Scratch is free.” No omitted-risk checklist or implied safety claim. | `go test ./internal/pipeline -run TestParentReasonRejectsRecordedUnsupportedProse/ok_omission_checklist -count=1`; outside-case replay and `TestPublishedParentReasonsRegression`. |
| L2 | An optional diagnostic observer receives a caller-owned copy of the completed-check reason map before public prose replaces it. Evaluation reads that snapshot. No card schema changes; observer map mutations cannot alter grades or reasons. Diagnoses were regenerated. | `go test ./internal/pipeline -run TestCheckedReasonsSnapshotBeforeParentProse -count=1`; `go test ./tools/eval -run TestDiagnoseUsesCheckedSnapshotRatherThanParentProse -count=1`; fixture Minecraft money/age/content now show `entail: flag_not_earned`, not `stood`. |

`rejection-probes/requests.jsonl` preserves the exact five distinct current checker requests, including messages and model options. The real checker replies alongside it all return false: unsupported wrong-group/cause; generalized content possibility; omitted-risk checklist; and an invented advertising use. `TestParentReasonRejectsRecordedUnsupportedProse` replays those exact requests through the production checker method. These are narrow independent rejection measurements, not fabricated successful model replies. Normal accepted example replays use separate real writer/checker recordings.

### Final sample accounting

Across eleven cases: **16 specific checked explanations, 0 cautious fallbacks, 50 unknown evidence gaps**. The five formerly generic areas now describe optional lootboxes (fixture Roblox), the store's limited COPPA claim (gather-1 Khan), the 10-year-old reviewer's Minecoin purchase (gather-2 Minecraft), reported App Store payment prompt and $46 charge (gather-2 Roblox), and third-party image processing/unspecified retention (gather-2 Roblox). The full published text for every area appears above, without shortening or substituting friendlier sample text.

All eleven before/after evidence hashes, verdicts and grade fields other than reason are identical, including settings and citation arrays. Unknowns remain unknown; the original four required rejection pins, both live Roblox age flags, and completed-child-purchase flags remain covered by existing tests. No title-specific rule or replacement reason is in product code. The runtime safe fallback is still tested even though these final examples no longer need it.

The whole-task cap includes every rejected pilot, superseded prose pass, repair, final pass and five rejection measurements. The ledger was never reset. Saved gathers were reused; there were no new Tavily calls. Final prose support remains model judgment, not deterministic semantic proof. User signoff of a sample remains pending.

### Verification

The final full `GOCACHE=/tmp/before-yes-go-cache ./tools/check.sh` passed Go formatting, vet, race tests, frontend lint/check/build and all **175 browser tests**. Log: `/tmp/t314-remediate-gate-final.log`. Two earlier full-gate attempts passed preceding stages but could not start browser tests because another task occupied port 4173; their failure logs remain `/tmp/t314-remediate-gate.log` and `/tmp/t314-remediate-gate-retry.log`. No other task was stopped and no port/test configuration was changed. The final complete run needed no bypass.

Direct source comparison against calibrated main `67501f0` found `judgeSystem`, `entailSystem`, `ageGateSystem`, `checkEntail`, `witnessedAgeGate`, `checkCitations` and `ground` identical. All changed paths are task-owned or its phase note. A count-only scan of the full worktree, including untracked recordings, found zero occurrences of each of the four configured secret values. No dependencies were added.


## Round 2 operator remediation

The user authorized direct remediation of M4 without another agent review cycle and explicitly requested approval with operator intervention. The round-2 review preserves its original finding and independent evidence, and now records that operator approval. The money-only support request requires preserving the difference between attempts to limit purchases and controls that were actually in place. A fifth recorded rejection probe rejects the unsupported “despite purchase limits” paraphrase. The refreshed writer/repair path selects the cited App Store payment prompt and reported $46 charge without the unsupported limit claim; no example replacement string is hard-coded in product code.

All eleven cases still have sixteen specific checked explanations, zero fallbacks and fifty insufficient-evidence areas. Full non-prose grade fields, evidence hashes and verdicts remain identical. The whole-task ledger includes both refresh attempts; `spend.json` gives final totals. All raw replies are retained. The user approved the Minecraft spending, Khan store-label and Scratch free-platform samples. The task stays in-review because its PR remains unmerged at the user’s request.
