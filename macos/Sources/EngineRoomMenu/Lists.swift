import EngineRoomKit
import SwiftUI

struct Chip: View {
    let text: String
    let color: Color
    var body: some View {
        Text(text).font(.system(size: 12))
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(Capsule().fill(color.opacity(0.15)))
            .foregroundColor(color == .secondary ? .secondary : color)
    }
}

/// Every repository the engine knows of, in four sections: pinned projects, registered ones by
/// category, folders here that are not registered, and repositories on GitHub that are not here. A
/// registered project opens its page.
struct ReposView: View {
    @EnvironmentObject var store: Store
    @State private var search = ""
    @State private var showGitHub = false
    @State private var showHidden = false

    /// One registered project as the list shows it, whether it is in a root or not.
    struct Registered: Identifiable, Hashable {
        let name: String
        let category: String
        let pinned: Bool
        let detail: String
        var id: String { name }
    }

    var body: some View {
        VStack(spacing: 0) {
            TextField("Search every root, here and on GitHub", text: $search)
                .textFieldStyle(.roundedBorder).padding(.horizontal, 12).padding(.bottom, 4)
            List {
                if let r = store.repos {
                    let registered = registeredOf(r).filter { matches($0.name, nil) }
                    let pinned = registered.filter(\.pinned)
                    if !pinned.isEmpty {
                        Section("Pinned") { ForEach(pinned) { ProjectRow(project: $0) } }
                    }
                    Section("Registered") {
                        if registered.isEmpty { Text("No registered project matches.").foregroundColor(.secondary) }
                        ForEach(byCategory(registered.filter { !$0.pinned }), id: \.0) { cat, items in
                            Text(cat.isEmpty ? "no category" : cat).font(.system(size: 13, weight: .semibold)).foregroundColor(.secondary)
                            ForEach(items) { ProjectRow(project: $0) }
                        }
                    }
                    let folders = r.roots.flatMap { l in l.repos.filter { $0.isUnregisteredFolder && matches($0.name, $0.description) }.map { (l.root, $0) } }
                    if !folders.isEmpty {
                        Section("Here, not registered") {
                            ForEach(folders, id: \.1.name) { root, repo in RepoRow(root: root, repo: repo) }
                        }
                    }
                    let remote = r.roots.flatMap { l in l.repos.filter { $0.isOnGitHubOnly && $0.hidden != true && matches($0.name, $0.description) }.map { (l.root, $0) } }
                    let hidden = r.roots.flatMap { l in l.repos.filter { $0.isOnGitHubOnly && $0.hidden == true && matches($0.name, $0.description) }.map { (l.root, $0) } }
                    Section {
                        if showGitHub || !search.isEmpty {
                            ForEach(remote, id: \.1.name) { root, repo in RepoRow(root: root, repo: repo) }
                        }
                    } header: {
                        HStack {
                            Text("On GitHub, not here (\(remote.count))")
                            Spacer()
                            Button(showGitHub || !search.isEmpty ? "Hide" : "Show") { showGitHub.toggle() }.buttonStyle(.link).disabled(!search.isEmpty).pointer()
                        }
                    }
                    if !hidden.isEmpty {
                        Section {
                            if showHidden {
                                ForEach(hidden, id: \.1.name) { root, repo in
                                    HStack {
                                        Text(repo.name).foregroundColor(.secondary)
                                        Spacer()
                                        Button("Show again") { Task { await store.runQuietly(["unhide", root.name, repo.name], saying: "Listing \(repo.name) again…") } }.controlSize(.small).pointer()
                                    }
                                }
                            }
                        } header: {
                            HStack {
                                Text("Hidden (\(hidden.count))")
                                Spacer()
                                Button(showHidden ? "Hide" : "Show") { showHidden.toggle() }.buttonStyle(.link).pointer()
                            }
                        }
                    }
                    ForEach(r.roots.compactMap { l in l.problem.map { (l.root, $0) } }, id: \.0.name) { root, p in
                        VStack(alignment: .leading) {
                            Text("\(root.name): \(p.reason)").foregroundColor(.red).font(.system(size: 13))
                            if let fix = p.fix { Text("Fix on the engine: \(fix)").font(.system(size: 13, design: .monospaced)) }
                        }
                    }
                    if r.roots.isEmpty {
                        Text("No roots are registered. A root is a folder for one GitHub owner's repositories:\nengineroom root add <folder> --owner <owner> --account <gh account>")
                            .font(.system(size: 13)).foregroundColor(.secondary)
                    }
                } else {
                    Text("Reading repositories …").foregroundColor(.secondary)
                }
            }
            .listStyle(.inset)
            Footer {
                Button("Add a repository…") { store.page = .add(root: nil) }.buttonStyle(.borderedProminent).pointer()
                Spacer()
                Text("Click a project to open its page").font(.system(size: 12)).foregroundColor(.secondary)
            }
        }
    }

    /// Every registered project once: those in a root from the root's listing, the rest from the
    /// projects in no root.
    private func registeredOf(_ r: ReposReply) -> [Registered] {
        var out: [Registered] = []
        for l in r.roots {
            for repo in l.repos where repo.isRegistered {
                out.append(Registered(name: repo.project ?? repo.name, category: repo.category ?? "", pinned: repo.pinned == true, detail: l.root.name))
            }
        }
        for p in r.projects {
            out.append(Registered(name: p.name, category: p.group ?? "", pinned: p.pinned == true, detail: "in no root"))
        }
        return out.sorted { $0.name.lowercased() < $1.name.lowercased() }
    }

    private func byCategory(_ items: [Registered]) -> [(String, [Registered])] {
        let groups = Dictionary(grouping: items, by: \.category)
        let named = groups.keys.filter { !$0.isEmpty }.sorted { $0.lowercased() < $1.lowercased() }
        var out = named.map { ($0, groups[$0]!) }
        if let none = groups[""] { out.append(("", none)) }
        return out
    }

    private func matches(_ name: String, _ description: String?) -> Bool {
        search.isEmpty || name.localizedCaseInsensitiveContains(search) || (description ?? "").localizedCaseInsensitiveContains(search)
    }
}

/// A registered project: click to open its page.
struct ProjectRow: View {
    @EnvironmentObject var store: Store
    let project: ReposView.Registered

    var body: some View {
        Button { Task { await store.openProject(project.name) } } label: {
            HStack {
                if project.pinned { Image(systemName: "pin.fill").foregroundColor(.orange).font(.system(size: 11)) }
                else { Circle().fill(Color.green).frame(width: 7, height: 7) }
                Text(project.name)
                Text(project.detail).font(.system(size: 12)).foregroundColor(.secondary)
                Spacer()
                let running = store.sessions.filter { $0.project == project.name && !$0.ended }.count
                if running > 0 { Chip(text: "\(running) running", color: .green) }
                Image(systemName: "chevron.right").foregroundColor(.secondary)
            }
            .contentShape(Rectangle())
        }.pointer()
        .buttonStyle(.plain)
    }
}

struct Legend: View {
    let color: Color
    let text: String
    var body: some View {
        HStack(spacing: 4) { Circle().fill(color).frame(width: 7, height: 7); Text(text).font(.system(size: 12)).foregroundColor(.secondary) }
    }
}

struct RepoRow: View {
    @EnvironmentObject var store: Store
    let root: Root
    let repo: Repo

    var body: some View {
        HStack {
            Circle().fill(repo.isRegistered ? Color.green : repo.isUnregisteredFolder ? Color.orange : Color.blue).frame(width: 7, height: 7)
            Text(repo.name)
            if let v = repo.visibility { Text(v).font(.system(size: 12)).foregroundColor(.secondary) }
            if let p = repo.project, p != repo.name { Text("as \(p)").font(.system(size: 12)).foregroundColor(.secondary) }
            Spacer()
            if repo.isRegistered {
                Button("Open") { Task { await store.openProject(repo.project ?? repo.name) } }.controlSize(.small).pointer()
            } else if repo.isUnregisteredFolder {
                Button("Register…") { store.page = .register(root: root.name, folder: root.path + "/" + repo.name) }.controlSize(.small).pointer()
            } else {
                Button("Get…") { store.page = .add(root: root.name + "/" + repo.name) }.controlSize(.small).pointer()
                Button { Task { await store.runQuietly(["hide", root.name, repo.name], saying: "Hiding \(repo.name)…") } } label: { Image(systemName: "eye.slash") }
                    .buttonStyle(.borderless).help("Hide it from this list; nothing changes on GitHub").pointer()
            }
        }
        .help(repo.description ?? "")
    }
}

/// Every action run on this machine, newest first, with its commands to copy: one scroll of cards,
/// a card per day, each action a line in it. An action opens in place to show its commands, so the
/// list keeps its place and nothing has to be read in a second pane. A failed action shows its
/// commands without being opened, since that is what it is looked at for.
struct HistoryView: View {
    @EnvironmentObject var store: Store
    @State private var open: Set<String> = []

    /// How many actions are listed; the history file keeps more, and the terminal interface reads it.
    private let shown = 200

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                if store.history.isEmpty {
                    Text("Nothing has been run on this machine yet. Every action the app or the terminal runs is listed here, with its commands to copy.")
                        .font(.system(size: 13)).foregroundColor(.secondary).padding(.top, 14)
                }
                ForEach(days, id: \.title) { day in
                    GroupTitle(day.title)
                    VStack(spacing: 0) {
                        ForEach(Array(day.entries.enumerated()), id: \.element.id) { i, e in
                            if i > 0 { Divider().padding(.vertical, 2) }
                            HistoryRow(entry: e, open: isOpen(e)) { toggle(e) }
                        }
                    }
                    .card()
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
    }

    /// The actions grouped by the day they started, newest day first, each named as a person
    /// would say it (Today, Yesterday, then the date).
    private var days: [(title: String, entries: [HistoryEntry])] {
        let cal = Calendar.current
        var out: [(title: String, entries: [HistoryEntry])] = []
        for e in store.history.prefix(shown) {
            let title: String
            if cal.isDateInToday(e.started) { title = "Today" }
            else if cal.isDateInYesterday(e.started) { title = "Yesterday" }
            else { title = e.started.formatted(.dateTime.weekday(.wide).day().month(.wide)) }
            if out.last?.title == title { out[out.count - 1].entries.append(e) } else { out.append((title, [e])) }
        }
        return out
    }

    private func isOpen(_ e: HistoryEntry) -> Bool { open.contains(e.id) != (e.status == "failed") }

    private func toggle(_ e: HistoryEntry) {
        if open.contains(e.id) { open.remove(e.id) } else { open.insert(e.id) }
    }
}

/// One action in the history: the time, its outcome as a dot and a word, its title and where it
/// ran, and when open its commands with a way to copy them all.
struct HistoryRow: View {
    @EnvironmentObject var store: Store
    let entry: HistoryEntry
    let open: Bool
    let toggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button(action: toggle) {
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Image(systemName: open ? "chevron.down" : "chevron.right")
                        .font(.system(size: 10, weight: .semibold)).foregroundColor(.secondary).frame(width: 12)
                    Text(entry.started, style: .time).font(.system(size: 13).monospacedDigit()).foregroundColor(.secondary).frame(width: 64, alignment: .leading)
                    Text(entry.title).font(.system(size: 13)).lineLimit(1).truncationMode(.tail)
                    Spacer(minLength: 8)
                    Text(entry.machine).font(.system(size: 12)).foregroundColor(.secondary).lineLimit(1)
                    HStack(spacing: 5) {
                        Circle().fill(color).frame(width: 7, height: 7)
                        Text(entry.status).font(.system(size: 12, weight: .medium)).foregroundColor(color)
                    }
                    .frame(width: 72, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).pointer()
            .help(open ? "Hide its commands" : "Show its commands")
            if open {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(Array((entry.notes ?? []).enumerated()), id: \.offset) { _, n in
                        Text(n).font(.system(size: 12)).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                    ForEach(Array(entry.commands.enumerated()), id: \.offset) { i, c in
                        CommandLine(index: i, text: c.display, status: c.status, output: c.output, why: c.why)
                    }
                    HStack(spacing: 10) {
                        LinkButton(title: entry.commands.count == 1 ? "Copy the command" : "Copy all \(entry.commands.count) commands") {
                            store.copy(joinAll(entry.commands.map(\.display)), label: entry.commands.count == 1 ? "the command" : "all \(entry.commands.count) commands")
                        }
                        if let c = store.copied { Text("Copied \(c).").font(.system(size: 12)).foregroundColor(.green) }
                    }
                }
                .padding(.leading, 22).padding(.bottom, 6)
            }
        }
        .padding(.vertical, 4)
    }

    private var color: Color {
        switch entry.status {
        case "done": return .green
        case "running": return .orange
        default: return .red
        }
    }
}

/// One command with its copy button, its outcome and any output.
struct CommandLine: View {
    @EnvironmentObject var store: Store
    let index: Int
    let text: String
    var status: String?
    var output: String?
    var why: String?
    var onFail: String?

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(label).font(.system(size: 12, weight: .semibold)).foregroundColor(color).frame(width: 64, alignment: .leading)
            VStack(alignment: .leading, spacing: 2) {
                Text(text).font(.system(size: 13, design: .monospaced)).textSelection(.enabled)
                if let why { Text(why).font(.system(size: 12)).foregroundColor(.secondary) }
                if let output, !output.isEmpty {
                    Text(output.split(separator: "\n").suffix(8).joined(separator: "\n"))
                        .font(.system(size: 12, design: .monospaced)).foregroundColor(status == "failed" ? .red : .secondary)
                }
                if let onFail { Text(onFail).font(.system(size: 13)).foregroundColor(.orange) }
            }
            Spacer(minLength: 0)
            Button { store.copy(text, label: "command \(index + 1)") } label: { Image(systemName: "doc.on.doc") }.pointer()
                .buttonStyle(.borderless).help("Copy this command")
        }
    }

    private var label: String {
        switch status {
        case nil, "pending": return "\(index + 1)."
        case "ok": return "ok, done"
        case "failed": return "FAILED"
        case "running": return "running"
        case "not run": return "not run"
        default: return status ?? ""
        }
    }

    private var color: Color {
        switch status {
        case "ok": return .green
        case "failed": return .red
        case "running": return .orange
        default: return .secondary
        }
    }
}
