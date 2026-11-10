import SwiftUI

// The app's one visual language, so every screen is drawn the same way (docs/decisions.md D-019,
// D-032):
//
// - a grey window with white cards, each with a hairline edge;
// - in a card, a header line (state light, name, a quiet kind) with the state in coloured words on
//   the right, and below it rows with a label column (Session, App, Open) beside their controls;
// - a button in words changes something (Join, Stop, Close); a button that is only an icon opens
//   something elsewhere or copies (VS Code, Finder, GitHub, the branch name); one button of a card
//   is filled, the one most often pressed;
// - a colour says one thing wherever it is used (Color.attention, .good, .bad, .busy);
// - links drawn as links; technical detail behind More info.
//
// The text sizes, the corner radii, the page gutters and the colours that carry a meaning are named
// in this file once, so two screens cannot drift apart by a point or a shade.

// MARK: sizes

/// The sizes text is drawn at, by what the text is for. A screen picks the purpose, never a number.
enum TextSize: CGFloat {
    /// A chevron or other small glyph beside a word.
    case glyph = 10
    /// Fine print: a list's section title, a command being read.
    case fine = 11
    /// Labels, chips, a state said in words.
    case caption = 12
    /// Detail beside or under a line of body text.
    case detail = 13
    /// What is read first on a line; the size both surfaces start from.
    case body = 14
    /// The name on a card or a list row.
    case name = 15
    /// The heading of the panel, of a project in it, and of an action.
    case heading = 18
    /// The title of a page in the window.
    case title = 22
}

extension Font {
    /// The system font at one of the app's sizes.
    static func app(_ size: TextSize, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        .system(size: size.rawValue, weight: weight, design: design)
    }
}

/// The corner radii, by what is rounded.
enum Radius {
    /// A card on a page.
    static let card: CGFloat = 10
    /// A compact card in the panel, and a banner.
    static let tile: CGFloat = 8
    /// The highlight of a chosen row.
    static let row: CGFloat = 6
    /// A word drawn as a badge.
    static let badge: CGFloat = 4
}

/// The space between a surface's edge and what it holds.
enum Gutter {
    /// A page in the window.
    static let page: CGFloat = 20
    /// The panel, and the pages narrow enough to be shown in it.
    static let panel: CGFloat = 12
}

// MARK: colours

extension Color {
    /// Something needs the developer: Claude is waiting, work would be lost, a step cannot run.
    /// Never used for anything that is merely happening.
    static let attention = Color.orange
    /// As it should be: a session running, an action done, something copied.
    static let good = Color.green
    /// It failed, or cannot be reached.
    static let bad = Color.red
    /// Under way and needing nothing: a command being run.
    static let busy = Color.accentColor
}

/// What a banner is saying, which picks its colour and its symbol.
enum Tone {
    case attention, good, bad

    var color: Color {
        switch self {
        case .attention: return .attention
        case .good: return .good
        case .bad: return .bad
        }
    }

    var symbol: String {
        switch self {
        case .attention: return "exclamationmark.triangle.fill"
        case .good: return "checkmark.circle.fill"
        case .bad: return "xmark.octagon.fill"
        }
    }
}

// MARK: cards

/// How much room a card gives what it holds: a page's card, or the panel's tighter one.
enum CardDensity { case regular, compact }

/// The card every item sits on.
struct CardStyle: ViewModifier {
    var density: CardDensity = .regular

    func body(content: Content) -> some View {
        let compact = density == .compact
        let radius = compact ? Radius.tile : Radius.card
        content
            .padding(.horizontal, compact ? 10 : 14).padding(.vertical, compact ? 8 : 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: radius).fill(Color(nsColor: .controlBackgroundColor)))
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(Color(nsColor: .separatorColor), lineWidth: 0.5))
    }
}

extension View {
    func card(_ density: CardDensity = .regular) -> some View { modifier(CardStyle(density: density)) }

    /// Draws a button filled when it is the one most often pressed on its card, and as every other
    /// button otherwise, so which of a row of buttons is filled can follow a setting.
    @ViewBuilder func filled(_ on: Bool) -> some View {
        if on { buttonStyle(.borderedProminent) } else { buttonStyle(.bordered) }
    }
}

/// What a session is doing, as the light and the words on the right of a card's header say it.
enum RunState {
    case waiting, running, idle

    var words: String {
        switch self {
        case .waiting: return "waiting for you"
        case .running: return "running"
        case .idle: return "not running"
        }
    }

    var color: Color {
        switch self {
        case .waiting: return .attention
        case .running: return .good
        case .idle: return Color.secondary
        }
    }
}

/// The light beside a project, a checkout or a session, the same on every screen: Claude's own mark
/// when Claude is waiting for the developer, a dot otherwise. A waiting item keeps its place in
/// its list (D-022), so the mark is how it is found, as the terminal list marks it (tui.ClaudeMark).
struct Light: View {
    let state: RunState
    /// Drawn larger in a page card's header than in a list, where it sits in a column 12 wide so
    /// the names beside the lights line up.
    var large = false

    var body: some View {
        if state == .waiting {
            Text("✻").font(.app(large ? .heading : .name, weight: .black)).foregroundColor(state.color)
                .fixedSize().frame(width: large ? nil : 12)
                .help("Claude is waiting for you")
        } else {
            // A dot has no baseline of its own; in a header laid out on the text's baseline it
            // sits a point above it, level with the middle of the name.
            Circle().fill(state.color).frame(width: 8, height: 8).frame(width: large ? nil : 12)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 1 }
        }
    }
}

/// A card's header line: the state light, the name (a link to a page when `open` is given), a quiet
/// word for what it is, and the state in coloured words on the right.
struct CardHeader<Trailing: View>: View {
    let state: RunState
    let name: String
    var kind: String? = nil
    var open: (() -> Void)? = nil
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Light(state: state, large: true)
            if let open {
                // A name that opens a page is drawn as a link, so it reads as clickable.
                Button(action: open) {
                    HStack(spacing: 3) {
                        Text(name).font(.app(.name, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                        Image(systemName: "chevron.right").font(.app(.glyph, weight: .semibold))
                    }
                    .foregroundColor(.accentColor)
                }
                .buttonStyle(.plain).pointer().help("Open its project's page").layoutPriority(1)
            } else {
                Text(name).font(.app(.name, weight: .semibold)).lineLimit(1).truncationMode(.middle).layoutPriority(1)
            }
            if let kind { Text(kind).font(.app(.caption)).foregroundColor(.secondary).lineLimit(1).truncationMode(.tail) }
            Spacer(minLength: 8)
            Text(state.words).font(.app(.caption, weight: .medium)).foregroundColor(state.color).lineLimit(1).fixedSize()
            trailing
        }
    }
}

extension CardHeader where Trailing == EmptyView {
    init(state: RunState, name: String, kind: String? = nil, open: (() -> Void)? = nil) {
        self.init(state: state, name: name, kind: kind, open: open) { EmptyView() }
    }
}

/// One row of a card: a label in the left column and its controls beside it. A row whose controls
/// wrap onto more lines keeps its label level with the first of them (`wraps`).
struct FieldRow<Content: View>: View {
    let label: String
    var wraps = false
    @ViewBuilder var content: Content
    init(_ label: String, wraps: Bool = false, @ViewBuilder content: () -> Content) {
        self.label = label
        self.wraps = wraps
        self.content = content()
    }
    var body: some View {
        HStack(alignment: wraps ? .top : .center, spacing: 10) {
            // Four points down puts the label on the first line of buttons, not above it.
            Text(label).font(.app(.caption)).foregroundColor(.secondary).frame(width: 52, alignment: .leading)
                .padding(.top, wraps ? 4 : 0)
            content
        }
    }
}

/// A fact about a checkout in a few words on a tinted capsule: what is unsaved, not pushed, behind
/// or merged, in the colour of what it means.
struct Chip: View {
    let text: String
    let color: Color
    var body: some View {
        Text(text).font(.app(.caption))
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(Capsule().fill(color.opacity(0.15)))
            .foregroundColor(color)
    }
}

/// A small heading over a group of cards.
struct GroupTitle<Trailing: View>: View {
    let title: String
    @ViewBuilder var trailing: Trailing
    var body: some View {
        HStack {
            Text(title).font(.app(.detail, weight: .semibold)).foregroundColor(.secondary)
            Spacer()
            trailing
        }
        .padding(.top, 6)
    }
}

extension GroupTitle where Trailing == EmptyView {
    init(_ title: String) { self.init(title: title) { EmptyView() } }
}

/// A small text control that does something on the page (More info): drawn in the accent colour,
/// as a link is, so it reads as clickable.
struct LinkButton: View {
    let title: String
    var systemImage: String? = nil
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 3) {
                Text(title)
                if let systemImage { Image(systemName: systemImage).font(.app(.glyph, weight: .semibold)) }
            }
            .font(.app(.caption)).foregroundColor(.accentColor)
        }
        .buttonStyle(.plain).pointer()
    }
}

// MARK: what a screen says before, or instead of, its content

/// What a screen must say before anything else: a problem, something that needs the developer, or
/// that something asked for is done. One line on a band tinted with what it means, its symbol
/// first, and on the right the buttons that deal with it. The words are in the text colour, not the
/// tint, so they are read as easily on a dark screen as on a light one.
struct Banner<Actions: View>: View {
    let tone: Tone
    let text: String
    @ViewBuilder var actions: Actions

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: tone.symbol).font(.app(.caption)).foregroundColor(tone.color)
            // The words take the room the buttons leave and wrap in it. The frame does that, not
            // a fixed vertical size: with one, the window's page was laid out taller than the
            // window and drew nothing (the window-problem snapshot shows this banner, on two lines).
            Text(text).font(.app(.detail)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            actions
        }
        .padding(.horizontal, 10).padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: Radius.tile).fill(tone.color.opacity(0.13)))
    }
}

extension Banner where Actions == EmptyView {
    init(_ tone: Tone, _ text: String) { self.init(tone: tone, text: text) { EmptyView() } }
}

/// What a screen says when it has nothing to show: what is missing, what to do about it, and the
/// button that does it, so an empty screen is never a dead end. With a tone it starts with that
/// tone's symbol, as a banner does: the screen is empty because something failed, not because
/// there is nothing yet.
struct EmptyState<Action: View>: View {
    var tone: Tone? = nil
    let title: String
    var detail: String? = nil
    @ViewBuilder var action: Action

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let tone { Image(systemName: tone.symbol).font(.app(.detail)).foregroundColor(tone.color) }
                Text(title).font(.app(.name, weight: .semibold))
            }
            if let detail {
                Text(detail).font(.app(.detail)).foregroundColor(.secondary)
            }
            action.padding(.top, 4)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension EmptyState where Action == EmptyView {
    init(tone: Tone? = nil, title: String, detail: String? = nil) { self.init(tone: tone, title: title, detail: detail) { EmptyView() } }
}

/// The title line of a page in the window: its title, a quiet word beside it, and on the right the
/// buttons that act on the page as a whole. Every page starts at the same place with the same size.
struct PageHeader<Trailing: View>: View {
    let title: String
    var detail: String? = nil
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(title).font(.app(.title, weight: .bold)).lineLimit(1)
            if let detail, !detail.isEmpty { Text(detail).font(.app(.detail)).foregroundColor(.secondary) }
            Spacer()
            trailing
        }
        .padding(.horizontal, Gutter.page).padding(.top, 16)
    }
}

extension PageHeader where Trailing == EmptyView {
    init(title: String, detail: String? = nil) { self.init(title: title, detail: detail) { EmptyView() } }
}
