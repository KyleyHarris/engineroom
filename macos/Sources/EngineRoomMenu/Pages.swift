import EngineRoomKit
import SwiftUI

/// Start a project's session one of three ways, or open it in VS Code (APP-START-001 to 003,
/// APP-EDIT-001). Claude Code always runs inside Zellij, so anything started can be joined later
/// from any machine.
struct StartView: View {
    @EnvironmentObject var store: Store
    let project: String
    @State private var mode = ""
    @State private var work = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            BackLink(label: "Repositories")
            Text(project).font(.system(size: 18, weight: .bold)).padding(.horizontal, 12)
            Text("Start Claude Code, always inside Zellij").font(.system(size: 13, weight: .semibold)).foregroundColor(.secondary).padding(.horizontal, 12)
            Picker("", selection: $mode) {
                Text("Zellij: start it and open it in \(store.status?.terminal ?? "the terminal")").tag("zellij")
                Text("Remote Control: start it, open no window; pick it up in the Claude app").tag("remote")
                Text("Both: Remote Control on, and open it in \(store.status?.terminal ?? "the terminal")").tag("both")
            }
            .pickerStyle(.radioGroup).labelsHidden().padding(.horizontal, 12)
            Text("On a new branch (optional)").font(.system(size: 13, weight: .semibold)).foregroundColor(.secondary).padding(.horizontal, 12).padding(.top, 6)
            TextField("name, e.g. fix-login", text: $work).textFieldStyle(.roundedBorder).padding(.horizontal, 12)
            Text("A worktree beside the main clone, on a new branch from the remote's main.").font(.system(size: 12)).foregroundColor(.secondary).padding(.horizontal, 12)
            Divider().padding(.vertical, 6)
            HStack {
                Image(systemName: "chevron.left.forwardslash.chevron.right")
                VStack(alignment: .leading) {
                    Text("Open in VS Code").bold()
                    Text(store.status?.isClient == true && store.status?.offline != true ? "the engine's folder, over SSH" : "the folder on this Mac").font(.system(size: 12)).foregroundColor(.secondary)
                }
                Spacer()
                Button("Open") { Task { await store.runNow(["edit-project", project], opensWindow: true) } }.pointer()
            }
            .padding(.horizontal, 12)
            Spacer()
            Footer {
                Text("The choice is remembered").font(.system(size: 12)).foregroundColor(.secondary)
                Spacer()
                Button("Show commands") { Task { await store.preview(action) } }.pointer()
                Button("Start") {
                    Task {
                        if mode != store.status?.start { await store.set("start", mode) }
                        // Starting with Remote Control alone opens no window, so the panel stays.
                        await store.runNow(action, opensWindow: mode != "remote")
                    }
                }.pointer()
                .buttonStyle(.borderedProminent)
            }
        }
        .onAppear { if mode.isEmpty { mode = store.status?.start ?? "zellij" } }
    }

    private var action: [String] {
        let w = work.trimmingCharacters(in: .whitespaces)
        return (w.isEmpty ? ["start", project] : ["new", project, w]) + ["--mode", mode]
    }
}

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
            .pickerStyle(.segmented).labelsHidden().padding(.horizontal, 12)
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
                            HStack { Text(r.name); Spacer(); Text(r.visibility ?? "").foregroundColor(.secondary).font(.system(size: 12)) }.tag(r.name)
                        }
                    }
                    .frame(height: 150)
                }
                CategoryField(category: $category)
                if create { Toggle("Public on GitHub", isOn: $isPublic) }
                if let l = roots.first(where: { $0.root.name == root }), !name.isEmpty {
                    LabeledContent("Folder") { Text("\(l.root.path)/\(name)").font(.system(size: 13, design: .monospaced)) }
                }
            }
            .padding(.horizontal, 12)
            Text(create ? "Makes the folder with a first commit, creates the repository on GitHub with the root's account, pushes, and registers it." : "Clones with the engine's own git and SSH key, then registers it. A category is a label in engineroom only; nothing moves.")
                .font(.system(size: 13)).foregroundColor(.secondary).padding(.horizontal, 12)
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
            Text("Register \((folder as NSString).lastPathComponent)").font(.system(size: 18, weight: .bold)).padding(.horizontal, 12)
            Text(folder).font(.system(size: 13, design: .monospaced)).foregroundColor(.secondary).padding(.horizontal, 12)
            Form { CategoryField(category: $category) }.padding(.horizontal, 12)
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
                    Text("Working out what this needs…").font(.system(size: 13)).foregroundColor(.secondary)
                }
            }
            ForEach(store.planProgress, id: \.side) { p in
                VStack(alignment: .leading, spacing: 3) {
                    if p.total > 0 {
                        ProgressView(value: Double(p.done), total: Double(p.total)) {
                            Text(p.done < p.total
                                 ? "Reading \(name(p.side)): \(p.done) of \(p.total) projects\(p.project.map { ", last \($0)" } ?? "")"
                                 : "Read \(name(p.side)): \(p.total) projects")
                                .font(.system(size: 13))
                        }
                        .animation(.easeOut(duration: 0.25), value: p.done)
                    } else {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Asking \(name(p.side)) what it holds that GitHub does not…").font(.system(size: 13))
                        }
                    }
                    if let c = p.command {
                        Text(c).font(.system(size: 11, design: .monospaced)).foregroundColor(.secondary)
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
            Text(store.planTitle.isEmpty ? "Getting the commands ready" : store.planTitle).font(.system(size: 18, weight: .bold)).padding(.horizontal, 12)
            // While the plan is made (a handover reads both Macs first), or a run reads them again
            // before its first step, a meter per machine says how far it has got; with nothing
            // counted yet, a spinner says the app is waiting, never a blank screen.
            if (store.planTitle.isEmpty && store.actionError == nil) || (store.running && store.stepStatus.isEmpty && !store.planProgress.isEmpty) {
                ReadingProgress().padding(.horizontal, 12)
            }
            if !store.planMachine.isEmpty {
                Text(store.running || store.finished != nil ? "on \(store.planMachine)" : "These commands will run on \(store.planMachine), in this order. Nothing has run yet.")
                    .font(.system(size: 13)).foregroundColor(.secondary).padding(.horizontal, 12)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    // A blocked plan ends with a heading saying it cannot run, and what to clear
                    // under it: both in orange, so they stand out from what it would have done.
                    let heading = store.planBlocked ? store.planNotes.lastIndex(where: { $0.hasPrefix("This handover cannot run yet") }) : nil
                    ForEach(Array(store.planNotes.enumerated()), id: \.offset) { i, n in
                        let blocker = heading.map { i >= $0 } ?? false
                        Text(n).font(.system(size: 13, weight: heading == i ? .semibold : .regular))
                            .foregroundColor(blocker ? .orange : .primary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if !store.planTitle.isEmpty && store.planLines.isEmpty && store.finished == nil && !store.planBlocked {
                        Text("Nothing to run.").foregroundColor(.secondary)
                    }
                    ForEach(Array(store.planLines.enumerated()), id: \.offset) { i, line in
                        CommandLine(index: i, text: line, status: store.stepStatus[i], output: store.stepOutput[i], onFail: store.stepFail[i])
                    }
                    if !store.planOffers.isEmpty && store.finished != true {
                        Text("You may also want:").font(.system(size: 13, weight: .semibold))
                        ForEach(Array(store.planOffers.enumerated()), id: \.offset) { _, o in
                            Button(o.label) { Task { await store.preview(o.action) } }.controlSize(.small).pointer()
                        }
                    }
                    if let e = store.actionError { Text(e).foregroundColor(.red) }
                    if store.finished == true { Text("Done.").foregroundColor(.green).bold() }
                    if store.finished == false && store.actionError == nil { Text("Stopped at the failed command; the commands after it were not run.").foregroundColor(.red) }
                    if let c = store.copied { Text("Copied \(c).").font(.system(size: 13)).foregroundColor(.green) }
                }
                .padding(12)
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
/// and on a client the engine and Offline (APP-TERM-001, APP-LOGIN-001, CONN-OFFLINE-001).
struct SettingsView: View {
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Settings for this Mac").font(.system(size: 22, weight: .bold)).padding(.horizontal, 20).padding(.top, 16)
            Form {
                if let s = store.status {
                    Section("This Mac") {
                        LabeledContent("Role") { Text(s.isClient ? "client of \(store.engineLabel(s))" : s.role) }
                        if s.isClient {
                            Toggle("Work local", isOn: Binding(get: { s.offline }, set: { v in Task { await store.switchOffline(v) } }))
                            Toggle("Forward every running session", isOn: Binding(get: { s.forward == true }, set: { v in Task { await store.setForward(v) } }))
                            Text("Each session also has its own Forward switch, and opening one of its links forwards it. Off closes every forward.")
                                .font(.system(size: 12)).foregroundColor(.secondary)
                        }
                        if !s.isClient {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Release to GitHub").bold()
                                    Text(s.released.map { "Last released \($0.formatted(date: .abbreviated, time: .shortened))" } ?? "Push the work checked out, and see what is uncommitted, before working local on a client.")
                                        .font(.system(size: 12)).foregroundColor(.secondary)
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
                                    .font(.system(size: 12)).foregroundColor(.secondary)
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
                        if let p = s.terminalProblem { Text(p).foregroundColor(.red) }
                        if let note = s.terminals.first(where: { $0.name == s.terminal })?.note { Text(note).font(.system(size: 13)).foregroundColor(.orange) }
                        Text("Found on this Mac. Also looked for \(s.terminals.filter { !$0.installed }.map(\.name).joined(separator: ", ")).")
                            .font(.system(size: 12)).foregroundColor(.secondary)
                    }
                    Section("New sessions") {
                        Picker("Start with", selection: Binding(get: { s.start }, set: { v in Task { await store.set("start", v) } })) {
                            Text("Zellij").tag("zellij"); Text("Remote Control").tag("remote"); Text("Both").tag("both")
                        }
                    }
                    Section("Your settings") {
                        HStack {
                            Text(store.settingsSync.isEmpty ? "Synced when you open engineroom, at most once an hour." : store.settingsSync)
                                .font(.system(size: 12)).foregroundColor(.secondary)
                            Spacer()
                            Button(store.settingsSyncing ? "Syncing…" : "Sync now") { Task { await store.syncSettings() } }.pointer()
                                .disabled(store.settingsSyncing)
                        }
                    }
                    Section {
                        DisclosureGroup("More info") {
                            Text(store.settingsStatus.isEmpty ? "Reading…" : store.settingsStatus)
                                .font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
                            Text("\(store.client?.executable ?? "engineroom not found") \(s.version ?? "")")
                                .font(.system(size: 11, design: .monospaced)).foregroundColor(.secondary).textSelection(.enabled)
                        }
                        .font(.system(size: 13))
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
