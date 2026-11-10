import AppKit
import EngineRoomKit
import SwiftUI

/// A registered project's page. The repository's own actions (add a worktree, its settings,
/// organise it) and, for each checkout (the main clone and every worktree), that checkout's
/// actions: join or start its session, open it in VS Code, start and stop its stack, open the
/// addresses it serves, and on a client forward its ports. Every button is an existing engineroom
/// action, shown or run as one.
struct ProjectPageView: View {
    @EnvironmentObject var store: Store
    let name: String
    @State private var work = ""
    @State private var category = ""
    @State private var addingWorktree = false
    /// New worktree's kind (D-024): for one branch, closed when it is done, or kept for a role,
    /// holding one branch after another; and, for a role, its name.
    @State private var forRole = false
    @State private var role = ""

    /// Cards side by side when the window is wide enough for two, one above the other otherwise.
    private let columns = [GridItem(.adaptive(minimum: 330), spacing: 12, alignment: .top)]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let p = store.project, p.name == name {
                header(p)
                ScrollView {
                    VStack(alignment: .leading, spacing: 10) {
                        if let problem = p.problem { Banner(.bad, problem) }
                        ForEach(p.checkouts.filter(\.main)) { c in CheckoutCard(project: p, checkout: c) }
                        worktrees(p)
                        if let loose = p.loose, !loose.isEmpty { looseServers(p, loose) }
                        GroupTitle("Project")
                        VStack(alignment: .leading, spacing: 10) { details(p) }.card()
                    }
                    .padding(.horizontal, Gutter.page).padding(.bottom, Gutter.page)
                }
            } else if let e = store.projectError {
                Banner(.bad, e).padding(Gutter.page)
                Spacer()
            } else {
                HStack { ProgressView().controlSize(.small); Text("Reading \(name)…").foregroundColor(.secondary) }.padding(Gutter.page)
                Spacer()
            }
        }
        .onAppear { category = store.project?.category ?? "" }
        .onChange(of: store.project?.category) { c in category = c ?? "" }
    }

    /// The project's name, its pin (a star that pins and unpins it at once), and the two things
    /// done to the project as a whole: ask Claude about it, and add a worktree.
    @ViewBuilder private func header(_ p: ProjectPage) -> some View {
        let pinned = store.pendingPin[name] ?? (p.pinned == true)
        PageHeader(title: name, detail: p.category) {
            Button { Task { await store.setPinned(name, !pinned) } } label: {
                Label(pinned ? "Pinned" : "Pin", systemImage: pinned ? "star.fill" : "star")
            }
            .help(pinned ? "Pinned to the panel; click to unpin" : "Pin to the panel, to start it from there").pointer()
            .disabled(store.working != nil)
            if let main = p.checkouts.first(where: \.main) {
                Button { ask(p, main) } label: { Label("Ask Claude", systemImage: "sparkles") }
                    .help("A Claude session that knows this project: say what you want done").pointer()
            }
        }
        .padding(.bottom, p.github == nil ? 10 : 2)
        if let g = p.github {
            // One line to the repository on GitHub, APP-PAGE-005.
            GitHubRow(label: g.repo, links: [("Code", g.code), ("Issues", g.issues), ("Pull requests", g.pulls)])
                .padding(.horizontal, Gutter.page).padding(.bottom, 10)
        }
    }

    private func createWorktree() {
        let w = work.trimmingCharacters(in: .whitespaces)
        let r = role.trimmingCharacters(in: .whitespaces)
        guard !w.isEmpty, !forRole || !r.isEmpty else { return }
        let mode = store.status?.start ?? "zellij"
        Task { await store.preview(forRole ? ["new-role", name, r, w, "--mode", mode] : ["new", name, w, "--mode", mode]) }
    }

    private func ask(_ p: ProjectPage, _ c: CheckoutView) {
        let mode = store.status?.start ?? "zellij"
        Task { await store.runNow(["assistant", p.name, c.path, "--mode", mode], opensWindow: mode != "remote") }
    }

    /// The worktrees, under a title that holds the one thing done to them as a group: adding one.
    @ViewBuilder private func worktrees(_ p: ProjectPage) -> some View {
        GroupTitle(title: "Worktrees") {
            Button { addingWorktree.toggle() } label: { Label("New worktree", systemImage: "plus") }.pointer()
        }
        if addingWorktree {
            // Which kind is chosen first, each saying what it means, so the choice is made knowing
            // its end.
            VStack(alignment: .leading, spacing: 8) {
                Picker("", selection: $forRole) {
                    Text("For a branch").tag(false)
                    Text("For a role").tag(true)
                }
                .pickerStyle(.segmented).labelsHidden().fixedSize()
                Text(forRole
                     ? "Kept: named for its role, it holds one branch after another, with the same session and ports."
                     : "For one piece of work: named for its branch, and closed when the branch is done.")
                    .font(.app(.caption)).foregroundColor(.secondary)
                HStack {
                    if forRole {
                        TextField("role, e.g. designer", text: $role).textFieldStyle(.roundedBorder).frame(maxWidth: 180)
                    }
                    TextField(forRole ? "its first branch, e.g. nav-rework" : "branch name, e.g. fix-login", text: $work)
                        .textFieldStyle(.roundedBorder).onSubmit(createWorktree)
                    Button("Create", action: createWorktree).buttonStyle(.borderedProminent).pointer()
                        .disabled(work.trimmingCharacters(in: .whitespaces).isEmpty || (forRole && role.trimmingCharacters(in: .whitespaces).isEmpty))
                }
            }
        }
        let others = p.checkouts.filter { !$0.main }
        if others.isEmpty {
            Text("None yet. A worktree is a second folder on its own branch, with its own session and ports.")
                .font(.app(.caption)).foregroundColor(.secondary)
        }
        LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
            ForEach(others) { c in CheckoutCard(project: p, checkout: c) }
        }
    }

    /// Servers on the project's ports that no running session owns (D-020): started by hand
    /// outside a session, or for a session that has ended and not been swept yet. Each has a
    /// Stop for that one process, shown before it runs; nothing here is stopped for the developer.
    @ViewBuilder private func looseServers(_ p: ProjectPage, _ loose: [LooseServer]) -> some View {
        GroupTitle("Running with no session")
        VStack(alignment: .leading, spacing: 10) {
            ForEach(loose) { s in
                HStack(alignment: .top, spacing: 10) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Port \(s.ports.map(String.init).joined(separator: ", ")) · pid \(s.pid)")
                            .font(.app(.detail, weight: .medium))
                        Text(s.command).font(.app(.fine, design: .monospaced)).foregroundColor(.secondary).lineLimit(1)
                        Text([s.folder, "since " + s.started].compactMap { $0 }.joined(separator: " · "))
                            .font(.app(.fine)).foregroundColor(.secondary)
                        if let session = s.session {
                            Text("Started for \(session), which has ended; the sweep stops it two minutes after.")
                                .font(.app(.fine)).foregroundColor(.attention)
                        }
                    }
                    Spacer()
                    Button("Stop") { Task { await store.preview(["stop-server", p.name, String(s.pid), String(s.ports[0])]) } }
                        .help("Stop this one process, only if it still listens on \(s.ports[0])").pointer()
                }
            }
        }
        .card()
    }

    // MARK: details, folded away

    /// What the project runs with and how it is listed, folded away until wanted: the scripts,
    /// ports and secret files from the settings, and organising it.
    @ViewBuilder private func details(_ p: ProjectPage) -> some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(p.scripts, id: \.name) { s in
                    LabeledRow(s.name, s.from.map { "\($0): \(s.path ?? "")" } ?? "none")
                }
                LabeledRow("ports", (p.reserve ?? []).isEmpty ? "none reserved" : "\((p.reserve ?? []).joined(separator: ", ")), \(p.slots) slot(s)")
                ForEach(p.secrets ?? [], id: \.name) { s in LabeledRow("secret", "\(s.name): \(s.state)") }
                LabeledRow("folder", p.settings)
                HStack {
                    Button("Open in VS Code") { openSettings(p.settings) }.pointer()
                    Button(store.settingsSyncing ? "Syncing…" : "Sync now") { Task { await store.syncSettings() } }.pointer()
                        .disabled(store.settingsSyncing)
                }
                .controlSize(.small)
            }
            .padding(.top, 4)
        } label: {
            Text("Scripts, ports and secrets").font(.app(.detail, weight: .medium))
        }
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    TextField("category", text: $category).textFieldStyle(.roundedBorder)
                    Button(store.working?.hasPrefix("Listing") == true ? "Saving…" : "Set category") { Task { await store.runQuietly(["category", name, category.trimmingCharacters(in: .whitespaces)], saying: "Listing \(name) under \(category.trimmingCharacters(in: .whitespaces))…") } }.pointer()
                        .disabled(category.trimmingCharacters(in: .whitespaces) == (p.category ?? ""))
                }
                Button("Unregister…") { Task { await store.preview(["unregister", name]) } }.pointer()
                    .help("Forget the project in engineroom. Its folder and worktrees are left as they are.")
                Text([p.identity, p.checkout].compactMap { $0 }.joined(separator: " · ")).font(.app(.fine)).foregroundColor(.secondary)
            }
            .padding(.top, 4)
        } label: {
            Text("Organise").font(.app(.detail, weight: .medium))
        }
    }

    /// The settings are a repository cloned on every machine, so they open here, on this Mac.
    /// open runs with an empty environment, so VS Code, when this starts it, gets the developer's
    /// own session environment as when opened from the Dock, not the app's (D-030).
    private func openSettings(_ path: String) {
        let full = (path as NSString).expandingTildeInPath
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        task.arguments = ["-a", "Visual Studio Code", full]
        task.environment = [:]
        try? task.run()
    }
}

/// One checkout on a card (Style.swift): its header (state, name, kind, state in words), then a row
/// each for its session, its app and the addresses it serves, and Ask Claude and More info at the
/// foot. Its branch, folder, slot and ports are under More info.
struct CheckoutCard: View {
    @EnvironmentObject var store: Store
    let project: ProjectPage
    let checkout: CheckoutView
    @State private var more = false
    @State private var nextBranching = false
    @State private var nextBranch = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Every action is a button in sight, none in a menu. Titled by the branch it has
            // checked out, the main clone too: "main clone" in the header already says which
            // checkout it is, and a main clone is often on another branch than main, which a fixed
            // "main" title hid. A role worktree is titled by its role, the branch it holds now
            // beside it in the kind (D-024); any other by the branch it has out.
            CardHeader(state: state, name: checkout.role ?? checkout.branch ?? (checkout.main ? "main clone" : (checkout.work ?? "worktree")), kind: kind)
            if let q = checkout.question, checkout.isWaiting { Text(q).font(.app(.detail)).foregroundColor(.attention) }
            if let clash = checkout.clash {
                Text(clash).font(.app(.caption)).foregroundColor(.attention).fixedSize(horizontal: false, vertical: true)
            } else {
                if checkout.running {
                    FieldRow("Session") {
                        Button("Join") { Task { await store.runNow(["join", checkout.session], opensWindow: true) } }
                            .buttonStyle(.borderedProminent).pointer()
                        Button("Stop") { Task { await store.preview(["stop", checkout.session]) } }.pointer()
                        Spacer()
                    }
                } else {
                    // The three ways to start it (APP-START-001 to 003), each a button in sight and
                    // none in a menu (D-025, D-032), in the words and the order of the Restart row
                    // a running session has. The way chosen in Settings is the filled one, so the
                    // usual start is still one click and the eye lands on it first. Short words
                    // that wrap rather than cut off, so all three read in a narrow card.
                    FieldRow("Start", wraps: true) {
                        FlowRow(spacing: 8) {
                            Button("\(terminal) + Remote") { start("both") }.filled(usual == "both").pointer()
                                .help("Start it with Remote Control, and open it in \(terminal)")
                            Button("\(terminal) only") { start("zellij") }.filled(usual == "zellij").pointer()
                                .help("Start it without Remote Control, and open it in \(terminal)")
                            Button("Remote only") { start("remote") }.filled(usual == "remote").pointer()
                                .help("Start it with Remote Control, and open no window")
                        }
                    }
                }
                // Restarting chooses how the session comes back, as starting does, each way a
                // button of its own; each ends the session and starts it fresh with the current
                // layout, and Claude continues its conversation. Remote Control is decided when
                // Claude starts, so a restart is how a running session gets it, or loses it.
                if checkout.running {
                    // Short words that wrap rather than cut off, so all three read in a narrow card.
                    FieldRow("Restart", wraps: true) {
                        FlowRow(spacing: 8) {
                            Button("\(terminal) + Remote") { restart("both") }.pointer()
                                .help("Restart it with Remote Control, and open it in \(terminal)")
                            Button("\(terminal) only") { restart("zellij") }.pointer()
                                .help("Restart it without Remote Control, and open it in \(terminal)")
                            Button("Remote only") { restart("remote") }.pointer()
                                .help("Restart it with Remote Control, and open no window")
                        }
                    }
                }
                if project.has("up") {
                    FieldRow("App") {
                        Button("Run") { Task { await store.runNow(["up", checkout.session]) } }
                            .help(checkout.running ? "Build and start everything this worktree needs, on its own ports" : "Start its session in the background, then build and start everything this worktree needs, on its own ports").pointer()
                        if project.has("down") {
                            Button("Stop") { Task { await store.runNow(["down", checkout.session]) } }
                                .help("Stop what Run started").pointer()
                        }
                        Spacer()
                    }
                }
                if let tasks = project.tasks, !tasks.isEmpty {
                    HStack(alignment: .top, spacing: 10) {
                        Text("Tasks").font(.app(.caption)).foregroundColor(.secondary).frame(width: 52, alignment: .leading).padding(.top, 4)
                        FlowRow(spacing: 8) {
                            ForEach(tasks, id: \.name) { t in
                                Button(t.name) { Task { await store.preview(["task", checkout.session, t.name]) } }
                                    .help(t.about ?? "This project's own task, run here").pointer()
                            }
                        }
                    }
                }
                if let role = checkout.role {
                    // Moving on, once a branch is done: the same folder, session and ports on the
                    // next branch, after the same check Close makes (D-024).
                    FieldRow("Branch") {
                        if nextBranching {
                            TextField("next branch", text: $nextBranch).textFieldStyle(.roundedBorder).frame(maxWidth: 200)
                                .onSubmit { moveOn(role) }
                            Button("Move on") { moveOn(role) }.pointer()
                                .disabled(nextBranch.trimmingCharacters(in: .whitespaces).isEmpty)
                        } else {
                            Button("Next branch…") { nextBranching = true }.pointer()
                                .help("Move this \(role) worktree on to its next branch; its changes must be saved or pushed first")
                        }
                        Spacer()
                    }
                }
                if let g = checkout.github {
                    // Its branch on GitHub, only once GitHub has it (APP-PAGE-006).
                    FieldRow("GitHub") {
                        GitHubRow(links: [("Branch", g.code)] + (g.pulls.map { [("Pull requests", $0)] } ?? []))
                        Spacer()
                    }
                }
                if !(checkout.links ?? []).isEmpty {
                    FieldRow("Open") {
                        LinksRow(links: checkout.links ?? [], session: checkout.session)
                        Spacer(minLength: 8)
                        if !(checkout.ports ?? []).isEmpty { ForwardSwitch(session: checkout.session) }
                    }
                }
            }
            // The code, Claude and cleaning up are buttons in sight, drawn like every other; the
            // row wraps rather than squeezing its words when the card is narrow.
            HStack(alignment: .top, spacing: 10) {
                Text("Code").font(.app(.caption)).foregroundColor(.secondary).frame(width: 52, alignment: .leading).padding(.top, 4)
                FlowRow(spacing: 8) {
                    Button { Task { await store.runNow(["edit-path", project.name, checkout.path], opensWindow: true) } } label: {
                        Label("VS Code", systemImage: "chevron.left.forwardslash.chevron.right")
                    }
                    .help("Open this folder in VS Code\(store.status?.isClient == true && store.status?.offline != true ? ", on the engine over SSH" : "")")
                    if store.filesHere {
                        Button { store.showInFinder(checkout.path) } label: { Label("Finder", systemImage: "folder") }
                            .help("Show this folder in Finder")
                    }
                    Button { ask() } label: { Label("Ask Claude", systemImage: "sparkles") }
                        .help("A Claude session that knows this \(checkout.main ? "project" : "worktree"): say what you want done")
                    if !checkout.main {
                        Button { Task { await store.preview(["clean-session", checkout.session]) } } label: { Label("Close", systemImage: "trash") }
                            .help("Stop its session, run the teardown script, remove the worktree; its commands are shown first, and it refuses while work would be lost, offering to save, put aside or push it")
                    }
                }
                .lineLimit(1).pointer()
            }
            HStack {
                Spacer()
                LinkButton(title: more ? "Less info" : "More info", systemImage: more ? "chevron.up" : "chevron.down") { more.toggle() }
            }
            if more {
                VStack(alignment: .leading, spacing: 4) {
                    LabeledRow("branch", checkout.branch ?? "none")
                    LabeledRow("folder", checkout.checkout)
                    if let ports = checkout.ports, !ports.isEmpty {
                        LabeledRow("slot", "\(checkout.slot ?? 0): " + ports.map { "\($0.name) \($0.port)" }.joined(separator: ", "))
                    }
                    LabeledRow("session", checkout.session)
                    if store.status?.isClient == true, store.status?.offline != true, let ports = checkout.ports, !ports.isEmpty {
                        ForwardRow(session: checkout.session, ports: ports, running: checkout.running)
                    }
                }
            }
        }
        .card()
    }

    /// What the checkout is, and how many files it has changed and not committed, if any: said
    /// in the header so the Session row keeps its room for its buttons.
    private var kind: String {
        let what = checkout.main ? "main clone" : (checkout.role != nil ? "role, on \(checkout.branch ?? "no branch")" : "worktree")
        return checkout.changes > 0 ? "\(what), \(checkout.changes) unsaved" : what
    }

    private var state: RunState {
        if checkout.isWaiting { return .waiting }
        return checkout.running ? .running : .idle
    }

    private var terminal: String { store.status?.terminal ?? "Terminal" }

    /// The way to start a session chosen in Settings, which the filled Start button is; in the
    /// terminal alone until the settings have been read, as every other start in the app assumes.
    private var usual: String { store.status?.start ?? "zellij" }

    /// Shows the move to the next branch first, as every action is shown.
    private func moveOn(_ role: String) {
        let b = nextBranch.trimmingCharacters(in: .whitespaces)
        guard !b.isEmpty else { return }
        nextBranching = false
        nextBranch = ""
        Task { await store.preview(["next-branch", project.name, role, b]) }
    }

    /// Shows the restart's commands first, as Restart always has, started the chosen way.
    private func restart(_ mode: String) {
        Task { await store.preview(["restart", checkout.session, "--mode", mode]) }
    }

    private func start(_ mode: String) {
        let action = (checkout.main ? ["start", project.name] : ["new", project.name, checkout.work ?? ""]) + ["--mode", mode]
        Task { await store.runNow(action, opensWindow: mode != "remote") }
    }

    private func ask() {
        let mode = store.status?.start ?? "zellij"
        Task { await store.runNow(["assistant", project.name, checkout.path, "--mode", mode], opensWindow: mode != "remote") }
    }
}

/// The addresses a checkout's stack serves, each drawn as a hyperlink that opens it in the browser,
/// with the port it is on, so the label never has to explain itself. Links look like links, not
/// buttons, so they read apart from the actions beside them. On a client, opening one forwards its
/// session's ports first when they are not yet (Store.open), so a link works with one click. The
/// labels are the project's own names for them (its settings' [links]).
struct LinksRow: View {
    @EnvironmentObject var store: Store
    let links: [EngineRoomKit.Link]
    /// The session the links belong to, forwarded on demand when one is opened on a client.
    var session: String? = nil
    var body: some View {
        if !links.isEmpty {
            FlowRow(spacing: 12) {
                ForEach(links, id: \.url) { l in
                    if let url = URL(string: l.url) {
                        Button { Task { await store.open(l, of: session) } } label: {
                            HStack(spacing: 2) {
                                Text(l.name).underline().foregroundColor(.accentColor)
                                Text(":\(url.port.map(String.init) ?? "")").foregroundColor(.secondary)
                            }
                            .font(.app(.detail))
                        }.pointer()
                        .buttonStyle(.plain)
                        .help("Open \(l.url) in the browser")
                    }
                }
            }
        }
    }
}

/// Links to pages on GitHub, drawn as hyperlinks like a stack's addresses (LinksRow), each opened
/// in the browser on this Mac: GitHub is reached directly, so nothing is forwarded. An optional
/// label (the repository's owner/name) leads the line.
struct GitHubRow: View {
    var label: String? = nil
    let links: [(String, String)]
    var body: some View {
        FlowRow(spacing: 12) {
            if let label { Text(label).font(.app(.caption)).foregroundColor(.secondary) }
            ForEach(links, id: \.1) { name, address in
                if let url = URL(string: address) {
                    Button { NSWorkspace.shared.open(url) } label: {
                        Text(name).underline().foregroundColor(.accentColor).font(.app(.detail))
                    }
                    .buttonStyle(.plain).pointer()
                    .help("Open \(address) in the browser")
                }
            }
        }
    }
}

/// Lays its children out left to right, wrapping onto the next line when the row is full.
struct FlowRow: Layout {
    var spacing: CGFloat = 6
    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > 0 && x + s.width > width { x = 0; y += line + spacing; line = 0 }
            x += s.width + spacing
            line = max(line, s.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + line)
    }
    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, line: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > bounds.minX && x + s.width > bounds.maxX { x = bounds.minX; y += line + spacing; line = 0 }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(s))
            x += s.width + spacing
            line = max(line, s.height)
        }
    }
}

/// On a client working on the engine: one session's forwarding switch, beside its links. Nothing on
/// the engine, or working local, where the ports are this Mac's own.
struct ForwardSwitch: View {
    @EnvironmentObject var store: Store
    let session: String
    var body: some View {
        if let s = store.status, s.isClient, !s.offline {
            HStack(spacing: 5) {
                Text("Forward").font(.app(.caption)).foregroundColor(.secondary)
                Toggle("Forward", isOn: Binding(get: { store.forwards(session) }, set: { v in Task { await store.setSessionForward(session, v) } }))
                    .toggleStyle(.switch).controlSize(.mini).labelsHidden()
            }
            .fixedSize()
            .help("Reach this session's ports on this Mac, at the same numbers, while the session runs")
            .pointer()
        }
    }
}

/// On a client: whether this checkout's ports are forwarded here, and why not when they are not.
struct ForwardRow: View {
    @EnvironmentObject var store: Store
    let session: String
    let ports: [EngineRoomKit.Port]
    let running: Bool

    var body: some View {
        let f = store.forwarding
        let wanted = f?.wants(session) == true
        let here = ports.filter { f?.forwarded[String($0.port)] == session }
        VStack(alignment: .leading, spacing: 2) {
            Text(summary(f, wanted: wanted, here: here)).font(.app(.caption)).foregroundColor(here.count == ports.count && running ? .good : .secondary)
            ForEach((f?.last?.notes ?? []).filter { $0.hasPrefix(session + "'s") }, id: \.self) { n in
                Text(n).font(.app(.caption)).foregroundColor(.attention)
            }
        }
    }

    private func summary(_ f: ForwardStatus?, wanted: Bool, here: [EngineRoomKit.Port]) -> String {
        guard let f else { return "Forwarding status is being read …" }
        if !wanted { return "Not forwarded to this Mac; switch Forward on, or open one of its links." }
        if !running { return "Forwarded only while its session runs." }
        if !f.connected { return "Not connected to the engine: " + (f.last?.error ?? "the forwarder has not connected yet.") }
        return "\(here.count) of \(ports.count) port(s) forwarded here."
    }
}

struct LabeledRow: View {
    let label: String
    let value: String
    init(_ label: String, _ value: String) { self.label = label; self.value = value }
    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label).font(.app(.detail, weight: .semibold)).frame(width: 64, alignment: .leading)
            Text(value).font(.app(.caption, design: .monospaced)).foregroundColor(.secondary).lineLimit(2).truncationMode(.middle)
        }
    }
}
