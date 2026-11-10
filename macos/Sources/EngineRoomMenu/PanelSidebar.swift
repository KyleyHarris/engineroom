import AppKit
import EngineRoomKit
import SwiftUI

/// The menu bar panel as a list and a detail (docs/decisions.md D-025).
///
/// The left is one row per project, in the panel's one fixed order (PanelProject.list, D-022), each
/// with one light for all its sessions: Claude's mark when any waits, green when one runs, grey
/// when none does. Pointing at a row, or clicking it, shows that project on the right and keeps it
/// there while the pointer moves across. The right is the project's GitHub line and one compact
/// card per checkout, running or not, with what is unsaved, unpushed, behind or merged, as the
/// feed sent it, and every action as a button; the project's Ask Claude sessions follow as one
/// line each. The project last chosen is remembered (Store.panelSelected).
struct PanelView: View {
    @EnvironmentObject var store: Store

    var body: some View {
        let list = PanelList(sessions: store.sessions, pinned: store.pinnedProjects)
        // The project last chosen stays shown, kept in the list even when nothing of it runs any
        // more; the first project when none was chosen yet.
        let projects = PanelProject.list(list, checkouts: store.checkouts, keep: store.panelSelected)
        let current = projects.first { $0.id == store.panelSelected } ?? projects.first
        VStack(spacing: 0) {
            banners
            if projects.isEmpty && list.here.isEmpty {
                Text("There are no sessions running. Start one from Repositories, or pin a project to keep it here.")
                    .foregroundColor(.secondary).padding(Gutter.panel)
                Spacer()
            } else {
                HStack(spacing: 0) {
                    sidebar(projects, here: list.here, current: current?.id)
                        .frame(width: 210)
                        .background(Color(nsColor: .underPageBackgroundColor).opacity(0.5))
                    Divider()
                    ScrollView {
                        if let current {
                            ProjectDetail(item: current).padding(Gutter.panel)
                        } else if !list.here.isEmpty {
                            HereDetail(sessions: list.here).padding(Gutter.panel)
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
            }
        }
    }

    /// What the panel must say before anything else: an engine on another build, an image just sent,
    /// a problem reading the engine. Each is a banner, the buttons that deal with it on its right.
    @ViewBuilder private var banners: some View {
        VStack(spacing: 6) {
            if let v = store.engineVersionDiffers {
                Banner(tone: .attention, text: v) {
                    // The machine that is behind is the one offered; when git cannot say which, both.
                    if store.engineBehind != "engine" {
                        Button("Upgrade this Mac…") { Task { await store.preview(["upgrade"]) } }.controlSize(.small).pointer()
                    }
                    if store.engineBehind != "here" {
                        Button("Upgrade the engine…") { Task { await store.preview(["upgrade", "--engine"]) } }.controlSize(.small).pointer()
                    }
                }
            }
            if let sent = store.imageSent, Date().timeIntervalSince(sent) < 120 {
                Banner(.good, "The image is on the engine's clipboard: paste it in the session now (Control-V in Claude Code).")
            }
            if let p = store.problem, !store.engineDown {
                Banner(.bad, p)
            }
        }
        // With nothing to say the stack is empty and takes no room, so the list starts at the top.
        .padding(.horizontal, Gutter.panel).padding(.bottom, bannerShown ? 6 : 0)
    }

    /// Whether any banner is drawn, so the room under them is given only when one is.
    private var bannerShown: Bool {
        if store.engineVersionDiffers != nil { return true }
        if let sent = store.imageSent, Date().timeIntervalSince(sent) < 120 { return true }
        return store.problem != nil && !store.engineDown
    }

    // MARK: the list on the left

    @ViewBuilder private func sidebar(_ projects: [PanelProject], here: [Session], current: String?) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 2) {
                let pinned = projects.filter(\.pinned), rest = projects.filter { !$0.pinned && !$0.recent }
                let recent = projects.filter(\.recent)
                if !pinned.isEmpty { SideTitle("Pinned"); ForEach(pinned) { row($0, current: current) } }
                if !rest.isEmpty { SideTitle("Running"); ForEach(rest) { row($0, current: current) } }
                if !recent.isEmpty { SideTitle("Last used"); ForEach(recent) { row($0, current: current) } }
                // This Mac's own sessions, started while it was offline, apart from the engine's
                // (CONN-HANDOFF-006); shown on the right only when nothing else is.
                if !here.isEmpty {
                    SideTitle("On this Mac")
                    ForEach(here) { s in
                        HStack(spacing: 9) {
                            Light(state: s.isWaiting ? .waiting : .running)
                            Text(s.name).font(.app(.body)).lineLimit(1).truncationMode(.middle)
                            Spacer(minLength: 4)
                            Button("Join") { Task { await store.runNow(["join"] + s.target, opensWindow: true) } }
                                .controlSize(.small).pointer()
                        }
                        .padding(.horizontal, 8).padding(.vertical, 5)
                    }
                }
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
        }
    }

    @ViewBuilder private func row(_ item: PanelProject, current: String?) -> some View {
        let on = item.id == current
        HStack(spacing: 9) {
            Light(state: RunState(item.light))
            Text(item.project).font(.app(.name, weight: on ? .semibold : .regular)).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 4)
            if item.sessions.count > 1 {
                Text("\(item.sessions.count)").font(.app(.fine, weight: .semibold)).foregroundColor(.secondary)
                    .padding(.horizontal, 5).padding(.vertical, 1)
                    .background(Capsule().fill(Color.secondary.opacity(0.15)))
                    .help("\(item.sessions.count) sessions running")
            }
        }
        .padding(.horizontal, 8).padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: Radius.row).fill(on ? Color.accentColor.opacity(0.22) : .clear))
        .contentShape(Rectangle())
        // Pointing shows it, and it stays shown while the pointer moves across to it: moving
        // straight right crosses no other row.
        .onHover { if $0 { store.panelSelected = item.id } }
        .onTapGesture { store.panelSelected = item.id }
        .pointer()
    }
}

/// The project on the right: its name (opening its page), its state, its GitHub line, a card per
/// checkout, and its Ask Claude sessions.
struct ProjectDetail: View {
    @EnvironmentObject var store: Store
    let item: PanelProject

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(item.project).font(.app(.heading, weight: .bold)).lineLimit(1)
                Button { Task { await store.openProject(item.project) } } label: {
                    HStack(spacing: 2) { Text("Page"); Image(systemName: "chevron.right").font(.app(.glyph, weight: .semibold)) }
                }
                .buttonStyle(.plain).foregroundColor(.accentColor).font(.app(.detail)).pointer()
                .help("Open its page in the window: worktrees, tasks, settings")
                Spacer()
                let state = RunState(item.light)
                Text(state.words).font(.app(.caption, weight: .medium)).foregroundColor(state.color)
            }
            if let g = store.repos?.github?[item.project] {
                GitHubRow(label: g.repo, links: [("Code", g.code), ("Issues", g.issues), ("Pull requests", g.pulls)])
            }
            if item.cards.isEmpty {
                // A pinned project with nothing running, before the feed has said its checkouts.
                HStack {
                    Text("Not running").foregroundColor(.secondary)
                    Spacer()
                    Button("Start") {
                        let mode = store.status?.start ?? "zellij"
                        Task { await store.runNow(["start", item.project, "--mode", mode], opensWindow: mode != "remote") }
                    }
                    .buttonStyle(.borderedProminent).pointer()
                }
                .card(.compact)
            }
            ForEach(item.cards) { CheckoutTile(project: item.project, card: $0) }
            ForEach(item.helpers) { h in
                HStack(spacing: 8) {
                    Image(systemName: "sparkles").foregroundColor(.secondary)
                    Text("Ask Claude").foregroundColor(.secondary)
                    if h.isWaiting { Light(state: .waiting) }
                    Spacer()
                    Button("Shut down") { Task { await store.preview(["stop"] + h.target) } }
                        .controlSize(.small).pointer().help("End this Ask Claude session; its commands are shown first")
                    Button("Open") { Task { await store.runNow(["join"] + h.target, opensWindow: true) } }
                        .controlSize(.small).pointer().help("Open it in your terminal")
                }
                .card(.compact)
            }
        }
    }
}

/// This Mac's own sessions on the right, when they are all the panel has.
struct HereDetail: View {
    @EnvironmentObject var store: Store
    let sessions: [Session]
    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text("Still running on this Mac, started while it was offline").font(.app(.detail, weight: .semibold)).foregroundColor(.secondary)
            ForEach(sessions) { s in
                HStack {
                    Light(state: s.isWaiting ? .waiting : .running)
                    Text(s.name)
                    Spacer()
                    Button("Join") { Task { await store.runNow(["join"] + s.target, opensWindow: true) } }
                        .buttonStyle(.borderedProminent).controlSize(.small).pointer()
                }
                .card(.compact)
            }
        }
    }
}

/// One checkout of the shown project (D-025): its light, which checkout it is, VS Code, Ask Claude,
/// its branch and pull requests on GitHub and copying the branch name as icons; then what is
/// unsaved, unpushed, behind or merged, and every action as a button in sight: Join, Restart and
/// Stop while it runs, or Start; Next branch for a role worktree; Close for any worktree. Claude's
/// question when it waits; its links and Forward.
struct CheckoutTile: View {
    @EnvironmentObject var store: Store
    let project: String
    let card: PanelProject.Card
    /// Asking for the branch a role worktree moves on to, in place, as its page does.
    @State private var nextBranching = false
    @State private var nextBranch = ""

    private var facts: CheckoutFacts? { card.facts }
    private var session: Session? { card.session }
    private var path: String? { facts?.path ?? session?.path }
    private var sessionName: String { facts?.session ?? session?.name ?? "" }
    private var isMain: Bool { facts?.main ?? (session?.name == project) }

    /// The main clone by its branch; a role worktree by its role; any other by its name.
    private var title: String {
        if isMain { return facts?.branch ?? "main clone" }
        if let role = facts?.role { return role }
        if let work = facts?.work { return work }
        if let s = session, s.name.hasPrefix(project + "-") { return String(s.name.dropFirst(project.count + 1)) }
        return session?.name ?? sessionName
    }

    /// What it is, quietly beside the title: the main clone, a role and the branch it holds now, or
    /// a worktree's branch when it is not its name.
    private var kind: String? {
        if isMain { return "main clone" }
        if facts?.role != nil { return facts?.branch.map { "role, on \($0)" } ?? "role" }
        if let b = facts?.branch, b != title { return b }
        return nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 7) {
                Light(state: RunState(card.light))
                Text(title).font(.app(.body, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                if let kind { Text(kind).font(.app(.caption)).foregroundColor(.secondary).lineLimit(1).truncationMode(.tail) }
                Spacer(minLength: 6)
                icons
            }
            HStack(spacing: 6) {
                if let f = facts {
                    if f.changes > 0 { Chip(text: "\(f.changes) unsaved", color: .attention) }
                    if f.unpushed > 0 { Chip(text: "\(f.unpushed) not pushed", color: .attention) }
                    if f.behind > 0 { Chip(text: "\(f.behind) behind main", color: .secondary) }
                    if f.isMerged { Chip(text: f.role != nil ? "merged: move on" : "merged: close it", color: .good) }
                }
                Spacer(minLength: 6)
                actions
            }
            if nextBranching, let role = facts?.role {
                HStack(spacing: 6) {
                    TextField("next branch, e.g. search-fix", text: $nextBranch).textFieldStyle(.roundedBorder)
                        .onSubmit { moveOn(role) }
                    Button("Move on") { moveOn(role) }.controlSize(.small).pointer()
                        .disabled(nextBranch.trimmingCharacters(in: .whitespaces).isEmpty)
                    Button("Cancel") { nextBranching = false }.controlSize(.small).pointer()
                }
            }
            if let s = session, let q = s.question, s.isWaiting {
                Text(q).font(.app(.caption)).foregroundColor(.attention)
            }
            // Its addresses and Forward whether or not its session runs: a stopped worktree's
            // switch takes effect when its session starts.
            let links = session?.links ?? facts?.links ?? []
            let ports = session?.ports ?? facts?.ports ?? []
            if !links.isEmpty || !ports.isEmpty || facts?.up == true || facts?.down == true {
                HStack(spacing: 8) {
                    LinksRow(links: links, session: sessionName)
                    Spacer(minLength: 6)
                    appButtons
                    if !ports.isEmpty { ForwardSwitch(session: sessionName) }
                }
            }
        }
        .card(.compact)
    }

    /// VS Code, Ask Claude, the branch and its pull requests on GitHub, and copying the branch
    /// name, as icons with tooltips.
    @ViewBuilder private var icons: some View {
        if let path {
            IconButton(symbol: "chevron.left.forwardslash.chevron.right", help: "Open in VS Code\(store.status?.isClient == true ? ", on the engine over SSH" : "")") {
                Task { await store.runNow(["edit-path", project, path], opensWindow: true) }
            }
            if store.filesHere {
                IconButton(symbol: "folder", help: "Show the folder in Finder") { store.showInFinder(path) }
            }
            IconButton(symbol: "sparkles", help: "Ask Claude about \(title)") {
                let mode = store.status?.start ?? "zellij"
                Task { await store.runNow(["assistant", project, path, "--mode", mode], opensWindow: mode != "remote") }
            }
        }
        if let g = session?.github {
            IconButton(symbol: "arrow.triangle.branch", help: "This branch on GitHub") { open(g.code) }
            if let p = g.pulls { IconButton(symbol: "arrow.triangle.pull", help: "Its pull requests on GitHub") { open(p) } }
        }
        if let b = facts?.branch {
            IconButton(symbol: "doc.on.doc", help: "Copy the branch name \(b)") { store.copy(b, label: b) }
        }
    }

    /// Every action in sight: Join, Restart (started again the way this Mac starts sessions; the
    /// page has the other ways) and Stop while it runs, or Start; Next branch for a role worktree
    /// and Close for any worktree, each shown before it runs where it changes something.
    @ViewBuilder private var actions: some View {
        if let s = session {
            Button("Join") { Task { await store.runNow(["join"] + s.target, opensWindow: true) } }
                .buttonStyle(.borderedProminent).controlSize(.small).pointer()
                .help("Join it in \(store.status?.terminal ?? "your terminal")")
            Button("Restart") {
                Task { await store.preview(["restart", s.name, "--mode", store.status?.start ?? "zellij"]) }
            }
            .controlSize(.small).pointer().help("End it and start it fresh, as set in Settings; its commands are shown first")
            Button("Stop") { Task { await store.preview(["stop"] + s.target) } }
                .controlSize(.small).pointer().help("End the session; its commands are shown first")
        } else {
            Button("Start") { start() }.buttonStyle(.borderedProminent).controlSize(.small).pointer()
                .help("Start its session, as set in Settings")
        }
        if !isMain, !sessionName.isEmpty {
            if facts?.role != nil {
                Button("Next branch") { nextBranching = true }.controlSize(.small).pointer()
                    .help("Move this role worktree on to its next branch; its work must be saved or pushed first")
            }
            Button("Close") { Task { await store.preview(["clean-session", sessionName]) } }
                .controlSize(.small).pointer()
                .help("Stop its session and remove the worktree; refused, with ways to keep it, while work would be lost")
        }
    }

    /// Run app and Stop app: the project's up and down scripts for this checkout, as its page's Run
    /// and Stop. Run app starts its session in the background first when it is not running. They
    /// sit on the card's app row, with the links they start and Forward, just under Join: beside
    /// Join, a role worktree's seven buttons squeezed every label to an ellipsis.
    @ViewBuilder private var appButtons: some View {
        if facts?.up == true, !sessionName.isEmpty {
            Button("Run app") { Task { await store.runNow(["up", sessionName, "--mode", store.status?.start ?? "zellij"]) } }
                .controlSize(.small).pointer()
                .help("Build and start everything this checkout needs, on its own ports\(session == nil ? ", starting its session in the background first" : "")")
        }
        if facts?.down == true, !sessionName.isEmpty {
            Button("Stop app") { Task { await store.runNow(["down", sessionName]) } }
                .controlSize(.small).pointer().help("Stop what Run app started")
        }
    }

    private func moveOn(_ role: String) {
        let b = nextBranch.trimmingCharacters(in: .whitespaces)
        guard !b.isEmpty else { return }
        nextBranching = false
        nextBranch = ""
        Task { await store.preview(["next-branch", project, role, b]) }
    }

    /// Starts the checkout's session as this Mac starts them: the main clone's by project, a
    /// worktree's by its work name (which opens the existing worktree).
    private func start() {
        let mode = store.status?.start ?? "zellij"
        let action = isMain ? ["start", project] : ["new", project, facts?.work ?? title]
        Task { await store.runNow(action + ["--mode", mode], opensWindow: mode != "remote") }
    }

    private func open(_ address: String) { if let u = URL(string: address) { NSWorkspace.shared.open(u) } }
}

extension RunState {
    /// The kit's light, as the state the app draws it in.
    init(_ light: PanelLight) {
        switch light {
        case .waiting: self = .waiting
        case .running: self = .running
        case .idle: self = .idle
        }
    }
}

/// A small icon button with a tooltip.
struct IconButton: View {
    let symbol: String
    let help: String
    let action: () -> Void
    var body: some View {
        Button(action: action) { Image(systemName: symbol).font(.app(.caption)) }
            .buttonStyle(.bordered).controlSize(.small).help(help).pointer()
    }
}

struct SideTitle: View {
    let title: String
    init(_ title: String) { self.title = title }
    var body: some View {
        Text(title).font(.app(.fine, weight: .semibold)).foregroundColor(.secondary)
            .padding(.horizontal, 8).padding(.top, 8).padding(.bottom, 2)
    }
}
