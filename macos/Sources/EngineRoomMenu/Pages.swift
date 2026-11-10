import EngineRoomKit
import SwiftUI

/// Get a repository from GitHub, or create a new one, into a chosen root and category
/// (APP-ADD-001, 002). Both show their commands before anything runs (APP-PREVIEW-001).
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
        VStack(alignment: .leading, spacing: 8) {
            BackLink(label: "Repositories")
            Picker("", selection: $create) {
                Text("Get from GitHub").tag(false)
                Text("Create new").tag(true)
            }
            .pickerStyle(.segmented).labelsHidden().padding(.horizontal, Gutter.panel)
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
            .padding(.horizontal, Gutter.panel)
            Text(create ? "Makes the folder with a first commit, creates the repository on GitHub with the root's account, pushes, and registers it." : "Clones with the engine's own git and SSH key, then registers it. A category is a label in engineroom only; nothing moves.")
                .font(.app(.detail)).foregroundColor(.secondary).padding(.horizontal, Gutter.panel)
            Spacer()
            Footer {
                Spacer()
                Button("Cancel") { store.close() }.pointer()
                Button("Show the commands") { Task { await store.preview(action) } }.pointer()
                    .buttonStyle(.borderedProminent).disabled(root.isEmpty || name.isEmpty)
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

/// Register a repository that is here but not registered (APP-REPOS-002).
struct RegisterView: View {
    @EnvironmentObject var store: Store
    let root: String
    let folder: String
    @State private var category = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            BackLink(label: "Repositories")
            Text("Register \((folder as NSString).lastPathComponent)").font(.app(.heading, weight: .bold)).padding(.horizontal, Gutter.panel)
            Text(folder).font(.app(.detail, design: .monospaced)).foregroundColor(.secondary).padding(.horizontal, Gutter.panel)
            Form { CategoryField(category: $category) }.padding(.horizontal, Gutter.panel)
            Spacer()
            Footer {
                Spacer()
                Button("Cancel") { store.close() }.pointer()
                Button("Show the commands") {
                    Task { await store.preview(["register", folder] + (category.isEmpty ? [] : ["--category", category])) }
                }.pointer()
                .buttonStyle(.borderedProminent)
            }
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

/// An action's commands: before they run (with Run), while they run, and after.
struct ActionView: View {
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            BackLink()
            Text(store.planTitle.isEmpty ? "Getting the commands ready" : store.planTitle).font(.app(.heading, weight: .bold)).padding(.horizontal, Gutter.panel)
            // While the plan is made (a handover reads both Macs first), or a run reads them again
            // before its first step, a meter per machine says how far it has got; with nothing
            // counted yet, a spinner says the app is waiting, never a blank screen.
            if (store.planTitle.isEmpty && store.actionError == nil) || (store.running && store.stepStatus.isEmpty && !store.planProgress.isEmpty) {
                ReadingProgress().padding(.horizontal, Gutter.panel)
            }
            if !store.planMachine.isEmpty {
                Text(store.running || store.finished != nil ? "on \(store.planMachine)" : "These commands will run on \(store.planMachine), in this order. Nothing has run yet.")
                    .font(.app(.detail)).foregroundColor(.secondary).padding(.horizontal, Gutter.panel)
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
                    ForEach(Array(store.planLines.enumerated()), id: \.offset) { i, line in
                        CommandLine(index: i, text: line, status: store.stepStatus[i], output: store.stepOutput[i], onFail: store.stepFail[i])
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
                .padding(Gutter.panel)
            }
            Footer {
                Button("Copy all (joined with &&)") { store.copy(joinAll(store.planLines), label: "all \(store.planLines.count) commands") }.pointer()
                    .disabled(store.planLines.isEmpty)
                Spacer()
                if store.finished == nil && !store.running {
                    Button("Back") { store.close() }.pointer()
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
/// (APP-TERM-001, APP-LOGIN-001, CONN-OFFLINE-001).
struct SettingsView: View {
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            PageHeader(title: "Settings for this Mac")
            Form {
                if let s = store.status {
                    Section("This Mac") {
                        LabeledContent("Role") { Text(s.isClient ? "client of \(store.engineLabel(s))" : s.role) }
                        if s.isClient {
                            // A button in words, as in the panel's header (D-032): it shows a
                            // handover that changes nothing until it is run, which a switch does
                            // not say. The words beside it say which state this Mac is in.
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(s.offline ? "Working local" : "Work local").bold()
                                    Text(s.offline
                                        ? "This Mac has the work. Hand it back to the engine when you are done."
                                        : "Take the work over from the engine and work on this Mac. Both Macs should be on, so the engine's work is pushed first.")
                                        .font(.app(.caption)).foregroundColor(.secondary)
                                }
                                Spacer()
                                Button(s.offline ? "Hand back to the engine" : "Work local") { Task { await store.switchOffline(!s.offline) } }.pointer()
                            }
                            Toggle("Forward every running session", isOn: Binding(get: { s.forward == true }, set: { v in Task { await store.setForward(v) } }))
                            Text("Each session also has its own Forward switch, and opening one of its links forwards it. Off closes every forward.")
                                .font(.app(.caption)).foregroundColor(.secondary)
                        }
                        if !s.isClient {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Release to GitHub").bold()
                                    Text(s.released.map { "Last released \($0.formatted(date: .abbreviated, time: .shortened))" } ?? "Push the work checked out, and see what is uncommitted, before working local on a client.")
                                        .font(.app(.caption)).foregroundColor(.secondary)
                                }
                                Spacer()
                                Button("Release…") { Task { await store.preview(["handoff", "release"]) } }.pointer()
                            }
                        }
                        // Upgrade runs through a Claude session on the machine upgraded, so a step that
                        // fails is diagnosed and corrected (internal/upgrade); the button shows the
                        // commands that start it first, as every action does.
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("engineroom \(s.version ?? "")").bold()
                                Text("Upgrade pulls the latest main, installs it and restarts the app, through a Claude session that fixes what gets in the way and asks before anything that could lose work.")
                                    .font(.app(.caption)).foregroundColor(.secondary)
                            }
                            Spacer()
                            Button("Upgrade this Mac…") { Task { await store.preview(["upgrade"]) } }.pointer()
                            if s.isClient && !s.offline {
                                Button("Upgrade the engine…") { Task { await store.preview(["upgrade", "--engine"]) } }.pointer()
                            }
                        }
                        Toggle("Open engineroom at login", isOn: Binding(get: { store.opensAtLogin }, set: { store.opensAtLogin = $0 }))
                    }
                    Section("Join sessions in") {
                        Picker("Terminal", selection: Binding(get: { s.terminal ?? "" }, set: { v in Task { await store.set("terminal", v) } })) {
                            ForEach(s.terminals.filter(\.installed)) { t in Text(t.name).tag(t.name) }
                        }
                        if let p = s.terminalProblem { Text(p).foregroundColor(.bad) }
                        if let note = s.terminals.first(where: { $0.name == s.terminal })?.note { Text(note).font(.app(.detail)).foregroundColor(.attention) }
                        Text("Found on this Mac. Also looked for \(s.terminals.filter { !$0.installed }.map(\.name).joined(separator: ", ")).")
                            .font(.app(.caption)).foregroundColor(.secondary)
                    }
                    Section("New sessions") {
                        Picker("Start with", selection: Binding(get: { s.start }, set: { v in Task { await store.set("start", v) } })) {
                            Text("Zellij").tag("zellij"); Text("Remote Control").tag("remote"); Text("Both").tag("both")
                        }
                    }
                    Section("Your settings") {
                        HStack {
                            Text(store.settingsSync.isEmpty ? "Synced when you open engineroom, at most once an hour." : store.settingsSync)
                                .font(.app(.caption)).foregroundColor(.secondary)
                            Spacer()
                            Button(store.settingsSyncing ? "Syncing…" : "Sync now") { Task { await store.syncSettings() } }.pointer()
                                .disabled(store.settingsSyncing)
                        }
                    }
                    Section {
                        DisclosureGroup("More info") {
                            Text(store.settingsStatus.isEmpty ? "Reading…" : store.settingsStatus)
                                .font(.app(.fine, design: .monospaced)).textSelection(.enabled)
                            Text("\(store.client?.executable ?? "engineroom not found") \(s.version ?? "")")
                                .font(.app(.fine, design: .monospaced)).foregroundColor(.secondary).textSelection(.enabled)
                        }
                        .font(.app(.detail))
                    }
                }
            }
            .formStyle(.grouped)
            Footer {
                Spacer()
                QuitButton()
            }
        }
    }
}
