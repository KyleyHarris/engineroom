import AppKit
import EngineRoomKit
import SwiftUI

/// Renders the app's real screens to PNG files from sample data, so how the app looks can be seen
/// and compared with its design after every change, without opening the menu bar panel on anyone's
/// screen. `--snapshot <dir> [dark]` writes one image per screen and exits: given to the built
/// product (`macos/.build/debug/EngineRoomMenu`) or to the installed app's executable, which
/// scripts/install.sh copies into the bundle as `engineroom-menu`.
///
/// Each screen is drawn in an offscreen window rather than with ImageRenderer, which cannot draw
/// the AppKit-backed controls (buttons, switches, menus) a SwiftUI window uses on macOS. The data
/// is invented (project-a, engine.example) and nothing is read from engineroom: the store has no client.
@MainActor
enum Snapshots {
    static func write(to dir: String, dark: Bool) {
        _ = NSApplication.shared
        NSApp.setActivationPolicy(.prohibited)
        NSApp.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let suffix = dark ? "-dark" : ""
        for (name, window, setUp) in screens {
            let store = Store(sample: true)
            setUp(store)
            if window {
                let view = MainWindow().environmentObject(store).frame(width: 980, height: 700)
                render(view, size: NSSize(width: 980, height: 700), to: "\(dir)/\(name)\(suffix).png")
            } else {
                let view = RootView().environmentObject(store).frame(width: 840, height: 480)
                    .background(Color(nsColor: .windowBackgroundColor))
                render(view, size: NSSize(width: 840, height: 480), to: "\(dir)/\(name)\(suffix).png")
            }
        }
    }

    /// The screens drawn: a name, whether it is the window (or the panel), and what the store
    /// holds for it.
    static let screens: [(String, Bool, (Store) -> Void)] = [
        ("panel-client", false, { s in s.status = sample(Status.self, statusClient); s.sessions = sessions; s.checkouts = checkouts; s.repos = repos; s.forwarding = sample(ForwardStatus.self, forwardJSON) }),
        ("panel-engine", false, { s in s.status = sample(Status.self, statusEngine); s.sessions = sessions; s.checkouts = checkouts; s.repos = repos }),
        ("window-project", true, { s in
            s.status = sample(Status.self, statusClient); s.sessions = sessions; s.repos = repos
            s.forwarding = sample(ForwardStatus.self, forwardJSON)
            s.project = sample(ProjectPage.self, projectJSON)
            s.windowItem = .project("project-a")
        }),
        ("window-repositories", true, { s in s.status = sample(Status.self, statusEngine); s.repos = repos; s.sessions = sessions; s.windowItem = .repositories }),
        ("window-history", true, { s in s.status = sample(Status.self, statusEngine); s.repos = repos; s.history = sample([HistoryEntry].self, historyJSON); s.windowItem = .history }),
        ("window-settings", true, { s in s.status = sample(Status.self, statusClient); s.repos = repos; s.windowItem = .settings }),
        // A handover being worked out: the engine half read, this Mac waiting to start.
        ("panel-reading", false, { s in
            s.status = sample(Status.self, statusClient); s.sessions = sessions; s.checkouts = checkouts; s.repos = repos
            s.page = .action(title: "")
            s.noteProgress(PlanProgress(side: "the engine", done: 7, total: 16, project: "project-c", command: "ssh -o BatchMode=yes engine.example -- 'engineroom handoff state --json'"))
            s.noteProgress(PlanProgress(side: "this machine", done: 0, total: 0))
        }),
        // Everything the panel can have to say first, at once, so the banners are seen in both
        // appearances: the engine on another build, an image just sent, a problem reading the engine.
        ("panel-notices", false, { s in
            s.status = sample(Status.self, statusClient); s.sessions = sessions; s.checkouts = checkouts; s.repos = repos
            s.engineVersionDiffers = "The engine runs another build of engineroom than this Mac (engine 0.9.1, here 0.9.0)."
            s.engineBehind = "here"
            s.imageSent = Date()
            s.problem = "The engine's answer could not be read: unexpected end of input"
        }),
        // Nothing running and nothing pinned: the panel's empty screen and its button.
        ("panel-empty", false, { s in s.status = sample(Status.self, statusClient) }),
        // The engine not answering: what happened, why, and the two ways on.
        ("panel-down", false, { s in
            s.status = sample(Status.self, statusClient)
            s.engineDown = true
            s.problem = "the engine did not answer: ssh: connect to host engine.example port 22: Operation timed out"
        }),
        // Working local: the header's badge and its button back, the banner, in the panel and on
        // the Settings page.
        ("panel-local", false, { s in s.status = sample(Status.self, statusLocal); s.sessions = sessions; s.checkouts = checkouts; s.repos = repos }),
        ("window-local", true, { s in s.status = sample(Status.self, statusLocal); s.repos = repos; s.windowItem = .settings }),
        // A project's page that could not be read: the page's own banner.
        ("window-problem", true, { s in
            s.status = sample(Status.self, statusClient); s.sessions = sessions; s.repos = repos
            s.projectError = "project-e is registered, but its folder is not on the engine. Get the repository onto the engine from the repositories list, or take the project off the list, and then read this page again."
            s.windowItem = .project("project-e")
        }),
    ]

    /// Draws a view in an offscreen window and writes it as a PNG at twice the size, as a Retina
    /// screen shows it.
    static func render<V: View>(_ view: V, size: NSSize, to path: String) {
        let host = NSHostingView(rootView: view)
        host.frame = NSRect(origin: .zero, size: size)
        let window = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = host
        window.layoutIfNeeded()
        // Let SwiftUI lay out and AppKit draw once before the picture is taken.
        RunLoop.main.run(until: Date().addingTimeInterval(0.4))
        guard let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) else { return }
        rep.size = host.bounds.size
        host.cacheDisplay(in: host.bounds, to: rep)
        if let png = rep.representation(using: .png, properties: [:]) {
            try? png.write(to: URL(fileURLWithPath: path))
        }
    }

    static func sample<T: Decodable>(_ type: T.Type, _ json: String) -> T {
        // A sample that no longer matches its shape is a defect in this file: fail loudly.
        try! EngineRoomClient.decode(type, data: Data(json.utf8), code: 0)
    }

    static var sessions: [Session] { sample(SessionsReply.self, sessionsJSON).sessions }

    /// Each checkout of the sample's pinned and running projects, as the feed sends them (D-025):
    /// project-a's main clone, its site-owner role worktree with work at risk, and a reports
    /// worktree whose branch is merged and nothing runs in.
    static var checkouts: [String: [CheckoutFacts]] { sample([String: [CheckoutFacts]].self, checkoutsJSON) }

    static let checkoutsJSON = """
    {"project-a":[
      {"session":"project-a","path":"/Users/dev/src/project-a","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0,"up":true,"down":true},
      {"session":"project-a-reports","path":"/Users/dev/src/project-a-reports","main":false,"work":"reports","branch":"reports","changes":0,"unpushed":0,"behind":3,"merged":true,"up":true,"down":true},
      {"session":"project-a-site-owner","path":"/Users/dev/src/project-a-site-owner","main":false,"work":"site-owner","role":"site-owner","branch":"site-owner-ui","changes":3,"unpushed":2,"behind":5,"up":true,"down":true}],
     "project-d":[{"session":"project-d","path":"/Users/dev/src/project-d","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0}],
     "project-b":[{"session":"project-b","path":"/Users/dev/src/project-b","main":true,"branch":"main","changes":1,"unpushed":0,"behind":0}],
     "service-x":[{"session":"service-x","path":"/Users/dev/work/service-x","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0}]}
    """
    static var repos: ReposReply { sample(ReposReply.self, reposJSON) }

    static let statusClient = """
    {"role":"client","engine":"engine.example","offline":false,"where":"the engine","start":"both","terminal":"Ghostty","terminals":[{"name":"Ghostty","bundle":"Ghostty.app"}],"forward":false,"forwarding":true}
    """
    /// The client of statusClient after it has taken the work over.
    static let statusLocal = """
    {"role":"client","engine":"engine.example","offline":true,"where":"this machine","start":"both","terminal":"Ghostty","terminals":[{"name":"Ghostty","bundle":"Ghostty.app"}],"forward":false,"forwarding":false}
    """
    static let statusEngine = """
    {"role":"engine","engine":"","offline":false,"where":"this machine","start":"both","terminal":"Ghostty","terminals":[{"name":"Ghostty","bundle":"Ghostty.app"}],"forward":false,"forwarding":false}
    """
    static let forwardJSON = """
    {"enabled":false,"connected":true,"forwarded":{"5180":"project-a","7202":"project-a"},"sessions":["project-a"]}
    """
    static let sessionsJSON = """
    {"read":"zellij list-sessions","sessions":[
     {"name":"project-a","project":"project-a","checkout":"~/src/project-a","path":"/Users/dev/src/project-a","ended":false,"waiting":"","created":"2h","slot":1,"ports":[{"name":"ui","port":5180},{"name":"api","port":7202}],"links":[{"name":"Web app","url":"http://localhost:5180/"},{"name":"API","url":"https://localhost:7202/"}],"github":{"code":"https://github.com/owner-a/project-a/tree/main"}},
     {"name":"project-a-site-owner","project":"project-a","checkout":"~/src/project-a-site-owner","path":"/Users/dev/src/project-a-site-owner","ended":false,"waiting":"waiting","question":"Claude is waiting for your input","created":"1h","slot":2,"ports":[{"name":"ui","port":5190},{"name":"api","port":7212}],"links":[{"name":"Web app","url":"http://localhost:5190/"},{"name":"API","url":"https://localhost:7212/"}],"github":{"code":"https://github.com/owner-a/project-a/tree/site-owner-ui","pulls":"https://github.com/owner-a/project-a/pulls?q=is%3Apr+head%3Asite-owner-ui"}},
     {"name":"project-b","project":"project-b","checkout":"~/src/project-b","path":"/Users/dev/src/project-b","ended":false,"waiting":"","created":"3d"},
     {"name":"service-x","project":"service-x","checkout":"~/work/service-x","path":"/Users/dev/work/service-x","ended":false,"waiting":"waiting","question":"Claude is waiting for your input","created":"20m","links":[{"name":"API","url":"https://localhost:7300/"}],"ports":[{"name":"api","port":7300}]},
     {"name":"project-a-ai","project":"project-a","checkout":"~/src/project-a","path":"/Users/dev/src/project-a","ended":false,"waiting":"","created":"5m","assistant":true}
    ]}
    """
    static let projectJSON = """
    {"name":"project-a","path":"/Users/dev/src/project-a","checkout":"~/src/project-a","category":"work","pinned":true,"identity":"owner-a/project-a","settings":"~/.config/engineroom/settings/projects/owner-a/project-a","reserve":["5180-5199","7201-7262"],"slots":4,
     "scripts":[{"name":"create","from":"your settings","path":"~/settings/create"},{"name":"up","from":"the repository","path":".engineroom/up"},{"name":"down","from":"the repository","path":".engineroom/down"},{"name":"teardown"}],
     "secrets":[{"name":"~/.config/project-a/secrets.json","state":"saved"}],
     "tasks":[{"name":"Pull a copy of the database","about":"copies the shared test database into this worktree's own"}],
     "checkouts":[
      {"session":"project-a","path":"/Users/dev/src/project-a","checkout":"~/src/project-a","branch":"main","main":true,"running":true,"ended":false,"changes":0,"slot":1,"ports":[{"name":"ui","port":5180},{"name":"api","port":7202}],"links":[{"name":"Web app","url":"http://localhost:5180/"},{"name":"API","url":"https://localhost:7202/"}]},
      {"session":"project-a-site-owner","path":"/Users/dev/src/project-a-site-owner","checkout":"~/src/project-a-site-owner","branch":"site-owner-ui","main":false,"work":"site-owner","running":true,"ended":false,"waiting":"waiting","question":"Claude is waiting for your input","changes":3,"slot":2,"ports":[{"name":"ui","port":5190},{"name":"api","port":7212}],"links":[{"name":"Web app","url":"http://localhost:5190/"},{"name":"API","url":"https://localhost:7212/"}]},
      {"session":"project-a-reports","path":"/Users/dev/src/project-a-reports","checkout":"~/src/project-a-reports","branch":"reports","main":false,"work":"reports","running":false,"ended":false,"changes":0,"slot":3,"ports":[{"name":"ui","port":5200},{"name":"api","port":7222}],"links":[{"name":"Web app","url":"http://localhost:5200/"}]}
     ],
     "loose":[{"pid":4242,"ports":[7232],"command":"dotnet run --launch-profile https","started":"Sat 7 Nov 09:12:00 2026","folder":"~/src/project-a-old","session":"project-a-old"}]}
    """
    static let reposJSON = """
    {"roots":[{"root":{"name":"src","path":"/Users/dev/src","owner":"owner-a","account":"owner-a"},"repos":[
      {"name":"project-a","description":"The main product","visibility":"private","here":"registered","project":"project-a","category":"work","pinned":true},
      {"name":"project-b","description":"A side project","visibility":"public","here":"registered","project":"project-b","category":"tools"},
      {"name":"project-c","description":"Not cloned here","visibility":"private"},
      {"name":"project-d","visibility":"private","here":"registered","project":"project-d","category":"tools","pinned":true},
      {"name":"project-e","visibility":"private","here":"registered","project":"project-e"}
    ]},{"root":{"name":"work","path":"/Users/dev/work","owner":"org-b","account":"owner-a"},"repos":[
      {"name":"service-x","visibility":"private","here":"registered","project":"service-x"},
      {"name":"service-y","visibility":"private","here":"registered","project":"service-y"}
    ]}],"projects":[],"github":{
      "project-a":{"repo":"owner-a/project-a","code":"https://github.com/owner-a/project-a","issues":"https://github.com/owner-a/project-a/issues","pulls":"https://github.com/owner-a/project-a/pulls"},
      "project-b":{"repo":"owner-a/project-b","code":"https://github.com/owner-a/project-b","issues":"https://github.com/owner-a/project-b/issues","pulls":"https://github.com/owner-a/project-b/pulls"},
      "project-d":{"repo":"owner-a/project-d","code":"https://github.com/owner-a/project-d","issues":"https://github.com/owner-a/project-d/issues","pulls":"https://github.com/owner-a/project-d/pulls"}}}
    """
    static let historyJSON = """
    [{"id":"1","started":"2026-11-07T09:00:00Z","ended":"2026-11-07T09:00:04Z","machine":"engine.example","title":"Start project-a","status":"done","commands":[{"display":"zellij attach --create-background project-a","why":"start the session","status":"ok","exit":0}]},
     {"id":"2","started":"2026-11-07T08:00:00Z","ended":"2026-11-07T08:00:02Z","machine":"engine.example","title":"Sync your settings","status":"failed","commands":[{"display":"git push -q","why":"send this machine's","status":"failed","exit":1,"output":"error: failed to push some refs"}]},
     {"id":"3","started":"2026-11-07T07:30:00Z","ended":"2026-11-07T07:30:03Z","machine":"engine.example","title":"Start the stack of project-a","status":"done","commands":[{"display":".engineroom/up","why":"start what the worktree needs","status":"ok","exit":0}]},
     {"id":"4","started":"2026-11-06T16:10:00Z","ended":"2026-11-06T16:10:01Z","machine":"laptop.example","title":"Forward the engine's ports","status":"done","commands":[{"display":"ssh -O forward -L 5180:localhost:5180 engine.example","why":"reach project-a's ui here","status":"ok","exit":0}]}]
    """
}
