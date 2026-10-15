#!/usr/bin/env python3
"""Compare committed parent-prose measurements; fail on any non-prose drift."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "testdata/eval/parent-reasons"

def load(name):
    return json.loads((DATA / name).read_text())

def cell(text):
    return str(text).replace("|", "\\|").replace("\n", " ")

def identity(grade):
    return {key: value for key, value in grade.items() if key != "reason"}

lines = ["# Parent-readable reasons evaluation", "",
"The calibrated grade prompt, area definitions, citation checks, setting grounding, original entailment prompts and independent age-event classifier are unchanged. A separate fast, metered writer can change only the final reason string. Every proposed factual explanation is checked against the exact cited quotes for support, source attribution and voice. One bounded repair gets the same check. An unknown area makes no title claim; rejected/missing/malformed prose uses a cautious fallback. No wording-only change alters a grade, setting, citation, evidence hash or verdict.", "",
"The before measurements use the judge from `67501f0` with the current measurement-only harness. Full collected findings are hashed in canonical JSON order; before/after hashes match. Outside-demo cases are judge-only probes using exact paragraphs verified against a saved comparison-guide capture; they are not new full research checks or independent confirmations of the guide's claims.", "",
"## Final comparisons against docs/voice.md", "",
"Every row below compares the actual published reason. ‘Specific report’ means a source-attributed statement; ‘fallback’ means the writer/checker did not produce a verified explanation and the card sends the parent to the source quotes. Unknowns use a plain evidence gap and explicitly avoid reassurance. Fallbacks are intentionally disclosed as incomplete specificity, rather than counted as successful generated explanations.", ""]
summary = []
for set_name in ("0", "1", "2", "outside"):
    before, after = load(f"before-{set_name}.json"), load(f"after-{set_name}.json")
    assert len(before) == len(after)
    specific = fallback = unknown = 0
    for b, a in zip(before, after):
        assert b["title"] == a["title"] and b["age"] == a["age"]
        assert b["evidence_sha256"] == a["evidence_sha256"], a["title"]
        assert b["got_verdict"] == a["got_verdict"], a["title"]
        assert not a.get("error"), a
        assert [identity(g) for g in b["grades"]] == [identity(g) for g in a["grades"]], a["title"]
        label = {"0": "Frozen fixtures", "1": "Saved gather 1", "2": "Saved gather 2", "outside": "Outside demo: judge-only"}[set_name]
        lines += [f"### {label}: {a['title']}, age {a['age']}", "", f"Verdict: **{b['got_verdict']} → {a['got_verdict']}**. Evidence SHA-256: `{a['evidence_sha256']}`. Grade/settings/citation changes: **0**.", "", "| Area / grade | Before | After | Voice assessment |", "|---|---|---|---|"]
        for old, new in zip(b["grades"], a["grades"]):
            reason = new["reason"]
            if new["level"] == "unknown":
                assessment = "Plain evidence gap; no factual title claim; no safety inference."
                unknown += 1
            elif "couldn’t verify a plain-language" in reason or reason.startswith("The cited sources raise"):
                assessment = "Cautious fallback; source quotes required; specificity not met."
                fallback += 1
            else:
                assessment = "Specific report; source attribution and concrete detail."
                specific += 1
            lines.append(f"| {new['area']} / {new['level']} | {cell(old.get('reason',''))} | {cell(reason)} | {assessment} |")
        delta = (a["judge_cost_nano"] - b["judge_cost_nano"]) / 1e9
        summary.append((label, a["title"], b["got_verdict"], a["got_verdict"], delta))
        lines += ["", f"Incremental metered judge cost for final prose: **${delta:.8f}** (reader/gather evidence reused).", ""]
    lines += [f"Set totals: **{specific} specific explanations; {fallback} cautious fallbacks; {unknown} unknown evidence gaps**. Specificity rate among non-unknown areas: **{specific}/{specific+fallback}**.", ""]
lines += ["## Cost and verdict accounting", "", "| Evidence set | Title | Before verdict | After verdict | Final prose cost increment |", "|---|---|---|---|---|"]
for row in summary:
    lines.append("| " + " | ".join(cell(v) for v in row[:-1]) + f" | ${row[-1]:.8f} |")
spend=load("spend.json")
lines += ["", f"Whole-task measured spend: **${spend['spent_usd']:.8f} / ${spend['cap_usd']:.2f}**. This includes rejected grading-prompt pilots, superseded prose attempts, repairs, outside-case baselines and the final pass. Final prose alone adds **${spend['final_prose_increment_nano']/1e9:.8f}** across the eleven cases; the rest is retained task experimentation, not hidden from the cap."]
lines += ["", "Final grade/verdict delta count: **0**. Final evidence, settings and citation delta count: **0**. The replay regression test checks these identities over the complete recordings, including both outside-demo cases.", "", "## Rejected grading-prompt approach", "", "Adding voice instructions directly to the grade prompt changed citations and semantic judgments. That approach was rejected; `failed-pilot.patch` records it, and `pilot/fixture.json` / `pilot/gather-1.json` retain the measured outputs. Those model replies remain in the recording store and their spend remains on the shared ledger. The final design reuses the original grading recordings and never asks the prose writer to regrade.", "", "| Set | Title | Area | Before | Rejected pilot | Explanation |", "|---|---|---|---|---|---|"]
for n, name in (("0", "fixture"), ("1", "gather-1")):
    for b, p in zip(load(f"before-{n}.json"), load(f"pilot/{name}.json")):
        for old, new in zip(b["areas"], p["areas"]):
            if old["got"] != new["got"]:
                why = "Voice-in-grade prompt changed the model's chosen citations/judgment; evidence was unchanged."
                if p["title"] == "Minecraft" and new["area"] == "content":
                    why += " It treated a review's disliked religious imagery as horror; rejected as unearned."
                if p["title"] == "Khan Academy Kids" and new["got"] == "ok":
                    why += " Reassurance did not establish the claimed absence or effectiveness."
                if p["title"] == "Roblox" and new["got"] == "unknown":
                    why += " Changed citation lists failed the original checks."
                lines.append(f"| {name} | {p['title']} | {new['area']} | {old['got']} | {new['got']} | {why} |")
        if b["got_verdict"] != p["got_verdict"]:
            lines.append(f"| {name} | {p['title']} | VERDICT | {b['got_verdict']} | {p['got_verdict']} | Follows the rejected area changes above; absent in the final isolated prose pass. |")
# Quote witnesses are taken from the final diagnoses, not rewritten by a model.
import re
quote_rows=[]
for n in ("0","1","2"):
    if n=="0": texts=[(DATA / "fixture-diagnosis.txt").read_text()]
    else: texts=[p.read_text() for p in sorted((ROOT / "testdata/eval/gathers" / n).glob("*/diagnosis.txt"))]
    for text in texts:
        title=area=""
        for line in text.splitlines():
            if line.startswith("diagnose "): title=line.split(" age ")[0][9:]
            match=re.match(r"  (\w+)  risks",line)
            if match: area=match[1]
            match=re.match(r"    quote (\S+) (\S+) ([^:]+): (.*)",line)
            if match: quote_rows.append((n,title,area,*match.groups()))
selected={
("0","Roblox","money"):"f_1f8ad057919885d2",
("0","Roblox","age_gating"):"f_3e1e4d584522e5a9",
("0","Roblox","developer_record"):"f_def610c472adb7ff",
("1","Roblox","age_gating"):"f_3e1e4d584522e5a9",
("2","Minecraft","money"):"f_4f0d24b3b6bb0948",
("2","Roblox","contact"):"f_936c4f1c94f5d7a9",
("2","Roblox","content"):"f_6fab8dae3bd61975",
("2","Roblox","money"):"f_1368a2c5a6fe539a",
("2","Roblox","data"):"f_37a8bd5d8f222dac",
("2","Roblox","age_gating"):"f_979e8a567e65fc3b",
("2","Roblox","developer_record"):"f_13ff2ba3714f5fe6",
}
lines += ["", "## Quote witnesses for every non-unknown area", "", "The complete citation sets remain in each diagnosis. These verbatim witnesses make the reason assessment inspectable; they are not replacement explanations. The age-gating witness below is the independently verified event, which can differ from the source report selected by the prose writer.", "", "| Set | Title / area | Source kind | Quoted witness |", "|---|---|---|---|"]
for n in ("0","1","2"):
    for result in load(f"after-{n}.json"):
        for g in result["grades"]:
            if g["level"]=="unknown": continue
            candidates=[row for row in quote_rows if row[:3]==(n,result["title"],g["area"])]
            chosen=next((row for row in candidates if row[3]==selected.get((n,result["title"],g["area"]))), candidates[0])
            lines.append(f"| {n} | {cell(result['title'])} / {g['area']} | {chosen[5]} | {cell(chosen[6])} |")
for case in load("outside-cases.json"):
    for f in case["findings"]:
        lines.append(f"| outside | {case['title']} / {f['area']} | {f['source']['kind']} | {cell(f['quote'])} |")
lines += ["", "## Evidence and limits", "",
"Saved gather diagnoses next to each capture show every final grade, original entailment result, age witness and complete cited quotes. Fixture diagnoses are reproducible with `-diagnose`; no saved title fixture is edited. The machine-readable before/after snapshots include the full grades and evidence hash, not merely the computed verdict.", "",
"The final prose checks are model judgments, not deterministic semantic proofs. A rejected explanation never changes the calibrated grade. The cautious fallback avoids introducing a factual claim but is less specific; the rates above make that limitation visible. Unknown areas remain unknown. A compliance label is described only as the source’s claim with an explicit limit; it does not establish actual privacy compliance.", "",
"Regression pins retain Minecraft fixture gameplay contact unknown, Minecraft gather-1 generic tracking unknown, Minecraft gather-2 child-account-only age mitigation unknown, and Khan gather-2 curriculum age unknown. Both live Roblox age flags retain the original independent event witness; the conditional-correction ablation and source-witness unit tests remain. The age writer receives the already validated source event as its focus; it cannot replace or alter that witness. Diagnostic causes are captured before public prose replaces the internal check explanations. Data, money and content prose is checked independently without the old copied-definition allowance; rejection leaves the earned grade and a cautious explanation.", "",
"## Reproduce without a key", "", "```sh", "for n in 0 1 2; do", "  go run ./tools/eval -models replay -gather \"$n\" -json", "done", "go run ./tools/eval -models replay -reason-cases testdata/eval/parent-reasons/outside-cases.json -json", "python3 tools/eval/parent_reason_report.py", "```", "",
"The live recording command uses `-models live -reuse-gather -live-cap-usd 0.50 -live-ledger /tmp/before-yes-t3.14-live-ledger.json`; the same ledger covered every pilot, repair and outside-case baseline. Do not reset it to re-run the task.", ""]
lines += ["", (DATA / "remediation.md").read_text(), ""]
(DATA / "report.md").write_text("\n".join(lines).rstrip()+"\n")
print("Wrote", DATA / "report.md")
