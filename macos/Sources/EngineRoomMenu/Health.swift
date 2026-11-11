import EngineRoomKit
import SwiftUI

/// The engine's health, a page of the window (D-019, D-037, HEALTH-PAGE-001): whether it is ready
/// to be worked on from away. It shows what `engineroom health` reports, item by item under the same
/// headings, each present or missing with what was found, how it was checked and, when missing, its
/// fix. The engine is checked when the page is opened and when Check again is pressed, never on a
/// timer, so the title line says when the items shown were checked.
///
/// A fix the program can run (starting a session's app) is offered as a button that shows its
/// commands first, as every action does (HEALTH-FIX-001); any other fix is words to read and copy,
/// with no way to run it from here (HEALTH-FIX-002).
struct HealthView: View {
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            PageHeader(title: "Health of the engine", detail: checkedLine) {
                if store.healthChecking { ProgressView().controlSize(.small) }
                Button("Check again") { Task { await store.checkHealth() } }.pointer()
                    .disabled(store.healthChecking)
                    .help("Check the engine now; it is never checked on a timer")
            }
            .padding(.bottom, 10)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    content
                }
                .padding(.horizontal, Gutter.page).padding(.bottom, Gutter.page)
            }
        }
    }

    /// Where and when the items shown were checked, beside the title.
    private var checkedLine: String? {
        guard let at = store.healthCheckedAt else { return nil }
        let time = at.formatted(date: .omitted, time: .shortened)
        // An engine not reached was asked and checked nothing, so the line says when it was tried.
        guard let r = store.health, (r.unreached ?? "").isEmpty else { return "tried at \(time)" }
        let place = r.machine == "this machine" ? "on this Mac" : "on \(store.engineName ?? r.machine)"
        return "checked \(place) at \(time)"
    }

    /// Whether this Mac does its actions here while the report is the engine's (a client working
    /// local), so the fixes it could run are offered as words only.
    private var workingLocal: Bool { store.status.map { $0.isClient && $0.offline } ?? false }

    @ViewBuilder private var content: some View {
        if let e = store.healthError {
            Banner(.bad, "The health check gave no report: \(e)")
        } else if let r = store.health {
            report(r)
        } else {
            EmptyState(title: "Checking the engine…",
                       detail: "Each item is read, and each running session's status script is run; nothing is changed.")
        }
    }

    @ViewBuilder private func report(_ r: HealthReport) -> some View {
        if let u = r.unreached, !u.isEmpty {
            // Nothing was checked, so no item is shown as present (HEALTH-CHECK-003).
            Banner(.bad, "The engine was not reached, so nothing was checked: \(u)")
        } else {
            summary(r)
            if workingLocal && r.items.contains(where: { $0.action != nil && !$0.present }) {
                Text("This Mac is working local, so a fix the program could run would run here, not on the engine. The fixes are shown to read; hand the work back to run them.")
                    .font(.app(.detail)).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            let sections = r.sections
            ForEach(Array(sections.enumerated()), id: \.offset) { i, s in
                // The sessions share one heading, each session's items on a card named for it.
                if s.isSession {
                    if i == 0 || !sections[i - 1].isSession { GroupTitle("Running sessions") }
                    ListCard(items: s.items, title: s.title) { HealthRow(item: $0, workingLocal: workingLocal) }
                } else {
                    GroupTitle(s.title)
                    ListCard(items: s.items) { HealthRow(item: $0, workingLocal: workingLocal) }
                }
            }
            if !sections.contains(where: \.isSession) {
                GroupTitle("Running sessions")
                Text("None running.").font(.app(.detail)).foregroundColor(.secondary).card()
            }
        }
    }

    /// Ready, or how many items are missing, first, as the command's last line says it.
    @ViewBuilder private func summary(_ r: HealthReport) -> some View {
        if r.ready {
            Banner(.good, "Ready: every item is present (\(r.items.count) checked). Nothing was changed.")
        } else {
            Banner(.attention, "Not ready: \(r.missing) of \(r.items.count) \(r.items.count == 1 ? "item is" : "items are") missing. Nothing was changed.")
        }
    }
}

/// One item of the health report: present or missing as a dot and a word, its name and what was
/// found, how it was checked, and when missing its fix, with the button that shows the fix's
/// commands when the program can run it.
struct HealthRow: View {
    @EnvironmentObject var store: Store
    let item: HealthItem
    let workingLocal: Bool

    private var color: Color { item.present ? .good : .attention }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                HStack(spacing: 5) {
                    Circle().fill(color).frame(width: 7, height: 7)
                    Text(item.present ? "present" : "missing").font(.app(.caption, weight: .medium)).foregroundColor(color)
                }
                .frame(width: 72, alignment: .leading)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.name).font(.app(.detail, weight: .semibold))
                    Text(item.found).font(.app(.detail)).foregroundColor(.secondary)
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if let action = item.runnableFix(workingLocal: workingLocal) {
                    // Shows the commands first; nothing runs until Run is pressed on that page.
                    Button("Fix…") { Task { await store.preview(action) } }.pointer()
                        .help("Show the commands that fix it, before anything runs")
                }
            }
            VStack(alignment: .leading, spacing: 3) {
                if !item.present, let fix = item.fix, !fix.isEmpty {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text("fix").font(.app(.caption, weight: .semibold)).foregroundColor(.attention)
                        Text(fix).font(.app(.detail)).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                    }
                }
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text("checked with").font(.app(.caption)).foregroundColor(.secondary)
                    Text(item.checked).font(.app(.fine, design: .monospaced)).foregroundColor(.secondary)
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(.leading, 82)
        }
    }
}
