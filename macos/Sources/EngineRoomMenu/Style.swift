import SwiftUI

// The app's one visual language, so every screen is drawn the same way:
//
// - a grey window with white cards, each with a hairline edge;
// - in a card, a header line (state dot, name, a quiet kind) with the state in coloured words on
//   the right, and below it rows with a label column (Session, App, Open) beside their controls;
// - one button style, words not icons, with only Join filled: the one thing most often done;
// - links drawn as links; technical detail behind More info.

/// The card every item sits on.
struct CardStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 14).padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 10).fill(Color(nsColor: .controlBackgroundColor)))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color(nsColor: .separatorColor), lineWidth: 0.5))
    }
}

extension View {
    func card() -> some View { modifier(CardStyle()) }
}

/// What a session is doing, as the dot and the words on the right of a card's header say it.
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
        case .waiting: return .orange
        case .running: return .green
        case .idle: return Color.secondary
        }
    }
}

/// A card's header line: the state dot, the name (a link to a page when `open` is given), a quiet
/// word for what it is, and the state in coloured words on the right.
struct CardHeader<Trailing: View>: View {
    let state: RunState
    let name: String
    var kind: String? = nil
    var open: (() -> Void)? = nil
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if state == .waiting {
                // Claude's own mark where the dot is: Claude is waiting for the developer. The
                // card keeps its place in the panel (D-022), so the mark is how it is found, as
                // the terminal list marks it (tui.ClaudeMark).
                Text("✻").font(.system(size: 18, weight: .black)).foregroundColor(state.color)
                    .fixedSize().help("Claude is waiting for you")
            } else {
                Circle().fill(state.color).frame(width: 8, height: 8).alignmentGuide(.firstTextBaseline) { $0[.bottom] - 1 }
            }
            if let open {
                // A name that opens a page is drawn as a link, so it reads as clickable.
                Button(action: open) {
                    HStack(spacing: 3) {
                        Text(name).font(.system(size: 15, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                        Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                    }
                    .foregroundColor(.accentColor)
                }
                .buttonStyle(.plain).pointer().help("Open its project's page").layoutPriority(1)
            } else {
                Text(name).font(.system(size: 15, weight: .semibold)).lineLimit(1).truncationMode(.middle).layoutPriority(1)
            }
            if let kind { Text(kind).font(.system(size: 12)).foregroundColor(.secondary).lineLimit(1).truncationMode(.tail) }
            Spacer(minLength: 8)
            Text(state.words).font(.system(size: 12, weight: .medium)).foregroundColor(state.color).lineLimit(1).fixedSize()
            trailing
        }
    }
}

extension CardHeader where Trailing == EmptyView {
    init(state: RunState, name: String, kind: String? = nil, open: (() -> Void)? = nil) {
        self.init(state: state, name: name, kind: kind, open: open) { EmptyView() }
    }
}

/// One row of a card: a label in the left column and its controls beside it.
struct FieldRow<Content: View>: View {
    let label: String
    @ViewBuilder var content: Content
    init(_ label: String, @ViewBuilder content: () -> Content) {
        self.label = label
        self.content = content()
    }
    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            Text(label).font(.system(size: 12)).foregroundColor(.secondary).frame(width: 52, alignment: .leading)
            content
        }
    }
}

/// A small heading over a group of cards.
struct GroupTitle<Trailing: View>: View {
    let title: String
    @ViewBuilder var trailing: Trailing
    var body: some View {
        HStack {
            Text(title).font(.system(size: 13, weight: .semibold)).foregroundColor(.secondary)
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
                if let systemImage { Image(systemName: systemImage).font(.system(size: 9, weight: .semibold)) }
            }
            .font(.system(size: 12)).foregroundColor(.accentColor)
        }
        .buttonStyle(.plain).pointer()
    }
}
