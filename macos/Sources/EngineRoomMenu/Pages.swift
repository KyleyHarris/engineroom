import EngineRoomKit
import SwiftUI

/// How wide the card of a form's fields grows before it stops.
enum FormWidth {
    static let card: CGFloat = 620
}

/// Get a repository from GitHub, or create a new one, into a chosen root and category
/// (APP-ADD-001, 002). Both show their commands before anything runs (APP-PREVIEW-001). A page of
/// the window, reached from the repositories: the link back, its title, and its fields on a card.
struct AddView: View {
    @EnvironmentObject var store: Store
    let initialRoot: String?
    @State private var create = false
    @State private var root = ""
    @State private var name = ""
    @State private var filter = ""
    @State private var category = ""
    @State private var isPublic = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            BackLink(label: "Repositories", gutter: Gutter.page).padding(.top, 12)
            PageHeader(title: "Add a repository", underLink: true).padding(.bottom, 10)
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Picker("", selection: $create) {
                        Text("Get from GitHub").tag(false)
                        Text("Create new").tag(true)
                    }
                    .pickerStyle(.segmented).labelsHidden().fixedSize()
                    // The fields, what running them does and the buttons on one card, so the
                    // button that goes on is beside what it acts on, not at the foot of the window.
                    VStack(alignment: .leading, spacing: 12) {
                        Form {
                            Picker("Root", selection: $root) {
                                ForEach(roots, id: \.root.name) { l in Text("\(l.root.name) · \(l.root.owner)").tag(l.root.name) }
                            }
                            if create {
                                TextField("Name", text: $name)
                            } else {
                                TextField("Repository", text: $filter, prompt: Text("filter the owner's repositories"))
                                List(selection: Binding(get: { name.isEmpty ? nil : name }, set: { name = $0 ?? "" })) {
                                    ForEach(candidates) { r in
                                        HStack { Text(r.name); Spacer(); Text(r.visibility ?? "").foregroundColor(.secondary).font(.app(.caption)) }.tag(r.name)
                                    }
                                }
                                .frame(height: 150)
                            }
                            CategoryField(category: $category)
                            if create { Toggle("Public on GitHub", isOn: $isPublic) }
                            if let l = roots.first(where: { $0.root.name == root }), !name.isEmpty {
                                LabeledContent("Folder") { Text("\(l.root.path)/\(name)").font(.app(.detail, design: .monospaced)) }
                            }
                        }
                        Text(create ? "Makes the folder with a first commit, creates the repository on GitHub with the root's account, pushes, and registers it." : "Clones with the engine's own git and SSH key, then registers it. A category is a label in engineroom only; nothing moves.")
                            .font(.app(.detail)).foregroundColor(.secondary)
                        HStack(spacing: 8) {
                            Spacer()
                            Button("Cancel") { store.close() }.pointer()
                            Button("Show the commands") { Task { await store.preview(action) } }.pointer()
                                .buttonStyle(.borderedProminent).disabled(root.isEmpty || name.isEmpty)
                        }
                    }
                    .card()
                }
                // A form reads badly stretched across a wide window, so its card stops at a width.
                .frame(maxWidth: FormWidth.card, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, Gutter.page).padding(.bottom, Gutter.page)
            }
        }
        .onAppear {
            if let r = initialRoot {
                let parts = r.split(separator: "/", maxSplits: 1).map(String.init)
                root = parts[0]
                if parts.count > 1 { name = parts[1]; filter = parts[1] }
            } else if root.isEmpty {
                root = roots.first?.root.name ?? ""
            }
        }
    }

    private var roots: [Listing] { store.repos?.roots ?? [] }

    private var candidates: [Repo] {
        (roots.first { $0.root.name == root }?.repos ?? []).filter {
            $0.isOnGitHubOnly && (filter.isEmpty || $0.name.localizedCaseInsensitiveContains(filter))
        }
    }

    private var action: [String] {
        var a = create ? ["create", root, name] : ["clone", root, name]
        if !category.isEmpty { a += ["--category", category] }
        if create && isPublic { a.append("--public") }
        return a
    }
}

/// A category: one already in use, or a new one typed in.
struct CategoryField: View {
    @EnvironmentObject var store: Store
    @Binding var category: String

    var body: some View {
        HStack {
            TextField("Category", text: $category, prompt: Text("optional"))
            Menu("Existing") {
                ForEach(store.categories, id: \.self) { c in Button(c) { category = c } }
                if store.categories.isEmpty { Text("none yet") }
            }
            .fixedSize()
        }
    }
}

/// Register a repository that is here but not registered (APP-REPOS-002). A page of the window, as
/// AddView is: the link back, its title, and the folder and its one field on a card.
struct RegisterView: View {
    @EnvironmentObject var store: Store
    let root: String
    let folder: String
    @State private var category = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            BackLink(label: "Repositories", gutter: Gutter.page).padding(.top, 12)
            PageHeader(title: "Register \((folder as NSString).lastPathComponent)", underLink: true).padding(.bottom, 10)
            VStack(alignment: .leading, spacing: 12) {
                Text(folder).font(.app(.detail, design: .monospaced)).foregroundColor(.secondary).textSelection(.enabled)
                Form { CategoryField(category: $category) }
                HStack(spacing: 8) {
                    Spacer()
                    Button("Cancel") { store.close() }.pointer()
                    Button("Show the commands") {
                        Task { await store.preview(["register", folder] + (category.isEmpty ? [] : ["--category", category])) }
                    }.pointer()
                    .buttonStyle(.borderedProminent)
                }
            }
            .card()
            .frame(maxWidth: FormWidth.card, alignment: .leading)
            .padding(.horizontal, Gutter.page)
            Spacer()
        }
    }
}

/// How far each machine's reading has got while a plan is made: a bar per machine with the project
/// last read and the command doing the reading, or a spinner while nothing has been counted.
struct ReadingProgress: View {
    @EnvironmentObject var store: Store

    /// The machine as the panel names it: the engine by its own name, this Mac as this Mac.
    private func name(_ side: String) -> String {
        switch side {
        case "the engine": return store.engineName ?? "the engine"
        case "this machine": return "this Mac"
        default: return side
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if store.planProgress.isEmpty {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Working out what this needs…").font(.app(.detail)).foregroundColor(.secondary)
                }
            }
            ForEach(store.planProgress, id: \.side) { p in
                VStack(alignment: .leading, spacing: 3) {
                    if p.total > 0 {
                        ProgressView(value: Double(p.done), total: Double(p.total)) {
                            Text(p.done < p.total
                                 ? "Reading \(name(p.side)): \(p.done) of \(p.total) projects\(p.project.map { ", last \($0)" } ?? "")"
                                 : "Read \(name(p.side)): \(p.total) projects")
                                .font(.app(.detail))
                        }
                        .animation(.easeOut(duration: 0.25), value: p.done)
                    } else {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Asking \(name(p.side)) what it holds that GitHub does not…").font(.app(.detail))
                        }
                    }
                    if let c = p.command {
                        Text(c).font(.app(.fine, design: .monospaced)).foregroundColor(.secondary)
                            .lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                    }
                }
            }
        }
    }
}

/// An action's commands: before they run (with Run), while they run, and after. The panel and the
/// window both show it, each at its own gutter. The link at the top is the one way back, in sight
/// before, during and after a run; the bar at the foot holds what goes on (Run, then Done).
struct ActionView: View {
    @EnvironmentObject var store: Store

    private var inWindow: Bool { store.actionIn == .window }
    private var gutter: CGFloat { inWindow ? Gutter.page : Gutter.panel }

    /// What the copy button says, in the words the history uses for the same thing.
    private var copyTitle: String {
        store.planLines.count == 1 ? "Copy the command" : "Copy all \(store.planLines.count) commands"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            BackLink(gutter: gutter).padding(.top, inWindow ? 12 : 0)
            Text(store.planTitle.isEmpty ? "Getting the commands ready" : store.planTitle).font(.app(.heading, weight: .bold)).padding(.horizontal, gutter)
            // While the plan is made (a handover reads both Macs first), or a run reads them again
            // before its first step, a meter per machine says how far it has got; with nothing
            // counted yet, a spinner says the app is waiting, never a blank screen.
            if (store.planTitle.isEmpty && store.actionError == nil) || (store.running && store.stepStatus.isEmpty && !store.planProgress.isEmpty) {
                ReadingProgress().padding(.horizontal, gutter)
            }
            if !store.planMachine.isEmpty {
                Text(store.running || store.finished != nil ? "on \(store.planMachine)" : "These commands will run on \(store.planMachine), in this order. Nothing has run yet.")
                    .font(.app(.detail)).foregroundColor(.secondary).padding(.horizontal, gutter)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    // A blocked plan ends with a heading saying it cannot run, and what to clear
                    // under it: both in the colour of what needs the developer, so they stand out from what it
                    // would have done.
                    let heading = store.planBlocked ? store.planNotes.lastIndex(where: { $0.hasPrefix("This handover cannot run yet") }) : nil
                    ForEach(Array(store.planNotes.enumerated()), id: \.offset) { i, n in
                        let blocker = heading.map { i >= $0 } ?? false
                        Text(n).font(.app(.detail, weight: heading == i ? .semibold : .regular))
                            .foregroundColor(blocker ? .attention : .primary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if !store.planTitle.isEmpty && store.planLines.isEmpty && store.finished == nil && !store.planBlocked {
                        Text("Nothing to run.").foregroundColor(.secondary)
                    }
                    // The commands on a card, as every list in the app is, apart from the notes
                    // above them and the outcome below.
                    if !store.planLines.isEmpty {
                        VStack(alignment: .leading, spacing: 8) {
                            ForEach(Array(store.planLines.enumerated()), id: \.offset) { i, line in
                                CommandLine(index: i, text: line, status: store.stepStatus[i], output: store.stepOutput[i], onFail: store.stepFail[i])
                            }
                        }
                        .card(inWindow ? .regular : .compact)
                    }
                    if !store.planOffers.isEmpty && store.finished != true {
                        Text("You may also want:").font(.app(.detail, weight: .semibold))
                        ForEach(Array(store.planOffers.enumerated()), id: \.offset) { _, o in
                            Button(o.label) { Task { await store.preview(o.action) } }.controlSize(.small).pointer()
                        }
                    }
                    if let e = store.actionError { Text(e).foregroundColor(.bad) }
                    if store.finished == true { Text("Done.").foregroundColor(.good).bold() }
                    if store.finished == false && store.actionError == nil { Text("Stopped at the failed command; the commands after it were not run.").foregroundColor(.bad) }
                    if let c = store.copied { Text("Copied \(c).").font(.app(.detail)).foregroundColor(.good) }
                }
                .padding(.horizontal, gutter).padding(.vertical, Gutter.panel)
            }
            Footer(gutter: gutter) {
                // How they are joined is said in the tooltip, not the label: the label says what
                // the button does, and the && is detail for whoever pastes them.
                Button(copyTitle) { store.copy(joinAll(store.planLines), label: store.planLines.count == 1 ? "the command" : "all \(store.planLines.count) commands") }.pointer()
                    .disabled(store.planLines.isEmpty)
                    .help("Joined with &&, so a paste stops at the first command that fails")
                Spacer()
                if store.finished == nil && !store.running {
                    Button("Run") { Task { await store.runPending() } }.pointer()
                        .buttonStyle(.borderedProminent).disabled(store.planLines.isEmpty)
                } else if store.running {
                    ProgressView().controlSize(.small)
                } else {
                    Button("Done") { store.close() }.buttonStyle(.borderedProminent).pointer()
                }
            }
        }
    }
}

/// This Mac's choices: the terminal (from those installed), how sessions start, opening at login,
/// and on a client the engine and the button that takes the work over or hands it back
/// (APP-TERM-001, APP-LOGIN-001, CONN-OFFLINE-001). Drawn as every page of the window is: groups
/// of cards on the grey window, each setting a row of its card with its control at the far end.
struct SettingsView: View {
    @EnvironmentObject var store: Store
    @State private var more = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            PageHeader(title: "Settings for this Mac").padding(.bottom, 10)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    if let s = store.status {
                        thisMac(s)
                        terminal(s)
                        GroupTitle("New sessions")
                        SettingRow(title: "Start with") {
                            Picker("Start with", selection: Binding(get: { s.start }, set: { v in Task { await store.set("start", v) } })) {
                                Text("Zellij").tag("zellij"); Text("Remote Control").tag("remote"); Text("Both").tag("both")
                            }
                            .labelsHidden().fixedSize()
                        }
                        .card()
                        yourSettings(s)
                    }
                    // Quit is at the foot of the page, under the last card and apart from every
                    // setting: it ends the app, where everything above it changes a choice.
                    HStack { Spacer(); QuitButton() }.padding(.top, 6)
                }
                .padding(.horizontal, Gutter.page).padding(.bottom, Gutter.page)
            }
        }
    }

    @ViewBuilder private func thisMac(_ s: Status) -> some View {
        GroupTitle("This Mac")
        VStack(alignment: .leading, spacing: 10) {
            SettingRow(title: "Role") {
                Text(s.isClient ? "client of \(store.engineLabel(s))" : s.role).foregroundColor(.secondary)
            }
            if s.isClient {
                Divider()
                // A button in words, as in the panel's header (D-032): it shows a handover that
                // changes nothing until it is run, which a switch does not say. The words beside
                // it say which state this Mac is in.
                SettingRow(title: s.offline ? "Working local" : "Work local",
                           caption: s.offline
                               ? "This Mac has the work. Hand it back to the engine when you are done."
                               : "Take the work over from the engine and work on this Mac. Both Macs should be on, so the engine's work is pushed first.") {
                    Button(s.offline ? "Hand back to the engine" : "Work local") { Task { await store.switchOffline(!s.offline) } }.pointer()
                }
                Divider()
                SettingRow(title: "Forward every running session",
                           caption: "Each session also has its own Forward switch, and opening one of its links forwards it. Off closes every forward.") {
                    Toggle("Forward every running session", isOn: Binding(get: { s.forward == true }, set: { v in Task { await store.setForward(v) } }))
                        .labelsHidden().toggleStyle(.switch)
                }
            }
            if !s.isClient {
                Divider()
                SettingRow(title: "Release to GitHub",
                           caption: s.released.map { "Last released \($0.formatted(date: .abbreviated, time: .shortened))" } ?? "Push the work checked out, and see what is uncommitted, before working local on a client.") {
                    Button("Release…") { Task { await store.preview(["handoff", "release"]) } }.pointer()
                }
            }
            Divider()
            // Upgrade runs through a Claude session on the machine upgraded, so a step that
            // fails is diagnosed and corrected (internal/upgrade); the button shows the
            // commands that start it first, as every action does.
            SettingRow(title: "engineroom \(s.version ?? "")",
                       caption: "Upgrade pulls the latest main, installs it and restarts the app, through a Claude session that fixes what gets in the way and asks before anything that could lose work.") {
                Button("Upgrade this Mac…") { Task { await store.preview(["upgrade"]) } }.pointer()
                if s.isClient && !s.offline {
                    Button("Upgrade the engine…") { Task { await store.preview(["upgrade", "--engine"]) } }.pointer()
                }
            }
            Divider()
            SettingRow(title: "Open engineroom at login") {
                Toggle("Open engineroom at login", isOn: Binding(get: { store.opensAtLogin }, set: { store.opensAtLogin = $0 }))
                    .labelsHidden().toggleStyle(.switch)
            }
        }
        .card()
    }

    @ViewBuilder private func terminal(_ s: Status) -> some View {
        GroupTitle("Join sessions in")
        VStack(alignment: .leading, spacing: 8) {
            // The terminals looked for and not found are named only when there are some.
            let missing = s.terminals.filter { !$0.installed }.map(\.name)
            SettingRow(title: "Terminal",
                       caption: "Found on this Mac." + (missing.isEmpty ? "" : " Also looked for \(missing.joined(separator: ", ")).")) {
                Picker("Terminal", selection: Binding(get: { s.terminal ?? "" }, set: { v in Task { await store.set("terminal", v) } })) {
                    ForEach(s.terminals.filter(\.installed)) { t in Text(t.name).tag(t.name) }
                }
                .labelsHidden().fixedSize()
            }
            if let p = s.terminalProblem { Text(p).foregroundColor(.bad) }
            if let note = s.terminals.first(where: { $0.name == s.terminal })?.note { Text(note).font(.app(.detail)).foregroundColor(.attention) }
        }
        .card()
    }

    @ViewBuilder private func yourSettings(_ s: Status) -> some View {
        GroupTitle("Your settings")
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(store.settingsSync.isEmpty ? "Synced when you open engineroom, at most once an hour." : store.settingsSync)
                    .font(.app(.caption)).foregroundColor(.secondary)
                Spacer()
                Button(store.settingsSyncing ? "Syncing…" : "Sync now") { Task { await store.syncSettings() } }.pointer()
                    .disabled(store.settingsSyncing)
            }
            // What the settings repository says of itself, and which engineroom this app runs:
            // technical detail, so behind More info as on a checkout's card.
            HStack {
                Spacer()
                LinkButton(title: more ? "Less info" : "More info", systemImage: more ? "chevron.up" : "chevron.down") { more.toggle() }
            }
            if more {
                Text(store.settingsStatus.isEmpty ? "Reading…" : store.settingsStatus)
                    .font(.app(.fine, design: .monospaced)).textSelection(.enabled)
                Text("\(store.client?.executable ?? "engineroom not found") \(s.version ?? "")")
                    .font(.app(.fine, design: .monospaced)).foregroundColor(.secondary).textSelection(.enabled)
            }
        }
        .card()
    }
}

/// One setting on a card: what it is in bold, a line under it saying what it does or where it
/// stands, and its control at the far end.
private struct SettingRow<Control: View>: View {
    let title: String
    var caption: String? = nil
    @ViewBuilder var control: Control

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).bold()
                if let caption { Text(caption).font(.app(.caption)).foregroundColor(.secondary) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 8) { control }
        }
    }
}
