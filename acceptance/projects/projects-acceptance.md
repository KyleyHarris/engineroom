# Projects - acceptance criteria

**Scope**: registering a repository as a {project.lower} the tool knows by name, listing the
registered {project.lowerPlural} under their {category.lowerPlural}, removing one, and bringing
repositories in from GitHub through {root.lowerPlural}: browsing an owner's repositories, cloning
and registering one, and creating a new one. Out of scope: per-{project.lower} git identity and
wiring Claude Code's hooks, which have their own issues.

**Sourcing**: every scenario's intent is stated in its own comment, or in the decisions it cites.
Nothing here is inferred from software.

**Status**: every scenario is `proposed`, written at planning before the software exists.
`proposed` is a proposal to be confirmed, `derived` is written up from delivered software,
`verified` means a person watched it happen, and `accepted` means it is confirmed as what the
software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Registers repositories on a machine, lists them, and removes them |

---

## User stories

> **As a** developer with many repositories, **I want** to declare once which repositories the
> tool knows, **so that** every other action can offer them by name instead of by path.

- Registering is one command, from a shell or from inside a Claude Code session.
- A folder that is not a repository, or one already registered, never becomes a second entry.
- Removing a {project.lower} forgets it and changes nothing on disk.

---

## Gherkin scenarios

### Feature: Registering a repository

<!-- intent: the developer declares which repositories are their projects, and registering one is a
     single step -->

```gherkin
@PROJ-ADD-001 @status:proposed @persona:Developer
Scenario: Registering a repository makes it a {project.lower} named after its folder
  Given a git repository on this machine that is not registered
  When the developer registers its folder
  Then it is listed among the registered {project.lowerPlural} under its folder's name
  And it can be opened by that name
```

<!-- intent: as PROJ-ADD-001 - a name is how every action refers to the project, so the developer
     chooses it when the folder's name will not do -->

```gherkin
@PROJ-ADD-002 @status:proposed @persona:Developer
Scenario: A {project.lower} can be registered under a name of the developer's choosing
  Given a git repository on this machine that is not registered
  When the developer registers its folder with the name "web"
  Then it is listed among the registered {project.lowerPlural} as "web"
```

<!-- intent: as PROJ-ADD-001 - only a repository can have sessions and worktrees -->

```gherkin
@PROJ-ADD-003 @status:proposed @persona:Developer
Scenario: A folder that is not a git repository is refused
  Given a folder on this machine that is not a git repository
  When the developer registers it
  Then they are told it is not a git repository
  And nothing is registered
```

<!-- intent: as PROJ-ADD-001 - one repository is one project -->

```gherkin
@PROJ-ADD-004 @status:proposed @persona:Developer
Scenario: Registering a repository twice keeps one {project.lower}
  Given a repository is already registered
  When the developer registers the same folder again
  Then they are told it is already registered, and under which name
  And it is listed once
```

<!-- intent: docs/decisions.md D-006 - a repository may declare its slots and setup scripts in a
     .engineroom folder, and registering it says what the tool found -->

```gherkin
@PROJ-ADD-005 @status:proposed @persona:Developer
Scenario: Registering a repository reports the setup it declares
  Given a git repository that declares a create script and a teardown script
  When the developer registers its folder
  Then they are told which setup scripts the repository declares
```

### Feature: Listing and removing projects

<!-- intent: as PROJ-ADD-001 -->

```gherkin
@PROJ-LIST-001 @status:proposed @persona:Developer
Scenario: The registered {project.lowerPlural} are listed with where they are
  Given two {project.lowerPlural} are registered
  When the developer lists the {project.lowerPlural}
  Then each is listed with its name and its folder
```

<!-- intent: nothing is deleted without a clear ask; removing a project from the tool is a change to
     the tool's own state only -->

```gherkin
@PROJ-REMOVE-001 @status:proposed @persona:Developer
Scenario: Removing a {project.lower} forgets it and leaves its files alone
  Given a {project.lower} is registered
  When the developer removes it
  Then it is no longer listed among the registered {project.lowerPlural}
  And its folder and everything in it are unchanged
```

### Feature: Grouping projects by category

<!-- intent: the project list is organised by category: a {category.lower} is a label kept in
     the program's own configuration, so it is set when registering and never moves a folder.
     Lists of repositories are in alphabetical order, ignoring case -->

```gherkin
@PROJ-CAT-001 @status:proposed @persona:Developer
Scenario: The {project.lowerPlural} are listed under their {category.lowerPlural}
  Given a {project.lower} registered in the {category.lower} "tools", two in the {category.lower} "web", and one with no {category.lower}
  When the developer opens the list of {project.lowerPlural}
  Then each {project.lower} is shown under a heading for its {category.lower}
  And under each heading the {project.lowerPlural} are in alphabetical order of their names, ignoring case
  And the {project.lower} with no {category.lower} is listed after the {category.lowerPlural}
```

<!-- intent: as PROJ-CAT-001 - a client and its engine are upgraded one at a time, so a client must
     still work with an engine that answers with the names alone -->

```gherkin
@PROJ-CAT-002 @status:proposed @persona:Developer
Scenario: A {client.lower} still lists the {project.lowerPlural} of an {engine.lower} that does not send {category.lowerPlural}
  Given an {engine.lower} that answers the list of {project.lowerPlural} with their names only
  When the developer on a {client.lower} opens the list of {project.lowerPlural}
  Then every {project.lower} is listed, with no {category.lower} headings
```

### Feature: Roots

<!-- intent: GitHub repositories are listed for an account by registering root folders: one folder
     per GitHub owner, with the account that can list it -->

```gherkin
@PROJ-ROOT-001 @status:proposed @persona:Developer
Scenario: Registering a {root.lower} ties a folder to a GitHub owner and account
  Given a folder on the {engine.lower} meant for one GitHub owner's repositories
  When the developer registers it as a {root.lower} for that owner, with the GitHub account that lists it
  Then it is listed among the {root.lowerPlural} with its folder, its owner and its account
```

<!-- intent: as PROJ-ROOT-001 - a root is where repositories are cloned, so a mistyped path is
     caught when it is registered, not at the first clone -->

```gherkin
@PROJ-ROOT-002 @status:proposed @persona:Developer
Scenario: A {root.lower} whose folder does not exist is refused
  Given there is no folder at the path the developer names
  When the developer registers that path as a {root.lower}
  Then they are told the folder does not exist
  And no {root.lower} is registered
```

### Feature: Browsing a root's repositories

<!-- intent: the developer sees what the owner has on GitHub and what is already here, and
     adds a repository to engineroom without leaving the program. The list is in alphabetical
     order, ignoring case -->

```gherkin
@PROJ-BROWSE-001 @status:proposed @persona:Developer
Scenario: A {root.lower}'s repositories are listed, with the ones already here marked
  Given a {root.lower} whose owner has three repositories on GitHub
  And one of them is cloned in the {root.lower} and registered as a {project.lower}
  When the developer browses the {root.lower}
  Then all three repositories are listed
  And they are listed in alphabetical order of their names, ignoring case
  And the one already here is marked as registered, under its {project.lower} name
```

<!-- intent: as PROJ-BROWSE-001, and the standing rule that the program reports a problem with its
     fix rather than failing silently -->

```gherkin
@PROJ-BROWSE-002 @status:proposed @persona:Developer
Scenario: When GitHub cannot be read, the reason and the fix are shown
  Given a {root.lower} whose GitHub account is not signed in on the {engine.lower}
  When the developer browses the {root.lower}
  Then they are told the owner's repositories could not be listed, and why
  And they are shown the command that signs the account in
```

### Feature: Adding a repository from GitHub

<!-- intent: a repository is added to engineroom by cloning it to a place and giving it a category,
     and is then ready to work on -->

```gherkin
@PROJ-CLONE-001 @status:proposed @persona:Developer
Scenario: Adding a listed repository clones it into the {root.lower} and registers it
  Given a {root.lower} whose owner has a repository called "widget" that is not on the {engine.lower}
  When the developer adds "widget" from the list, in the {category.lower} "tools"
  Then it is cloned into the {root.lower} as "widget"
  And it is registered as a {project.lower} called "widget" in the {category.lower} "tools"
```

<!-- intent: the standing rule that nothing outside the program's own state is changed unasked:
     a folder already in the way is the developer's, whatever it holds -->

```gherkin
@PROJ-CLONE-002 @status:proposed @persona:Developer
Scenario: A folder already in the {root.lower} under the repository's name is never cloned over
  Given a folder called "widget" is already in the {root.lower}
  When the developer adds the repository "widget" from the list
  Then they are told the folder is already there, and how to register it
  And nothing is cloned and the folder is unchanged
```

### Feature: Creating a repository

<!-- intent: a new GitHub repository can be created from engineroom and a local folder pushed into
     it -->

```gherkin
@PROJ-CREATE-001 @status:proposed @persona:Developer
Scenario: A new repository is made in the {root.lower}, created on GitHub, pushed and registered
  Given the owner of the {root.lower} has no repository called "gadget"
  When the developer creates "gadget" in the {root.lower} as a private repository, in the {category.lower} "tools"
  Then its folder is made in the {root.lower} with a first commit
  And a private repository "gadget" is created for the owner on GitHub, holding that commit
  And it is registered as a {project.lower} called "gadget" in the {category.lower} "tools"
```

<!-- intent: as PROJ-CREATE-001 - a create that fails half way would leave a folder or a GitHub
     repository behind, so a name that cannot work is refused first -->

```gherkin
@PROJ-CREATE-002 @status:proposed @persona:Developer
Scenario: A name the owner already has on GitHub is refused before anything is created
  Given the owner of the {root.lower} already has a repository called "widget" on GitHub
  When the developer creates "widget" in the {root.lower}
  Then they are told the owner already has a repository of that name
  And no folder is made and nothing is created on GitHub
```

### Feature: Asking whether a folder is a project's

<!-- intent: a script or a hook that runs on every command can ask whether a folder belongs to a
     registered project cheaply and by exit code, without reading engineroom's configuration file
     itself -->

```gherkin
@PROJ-HERE-001 @status:proposed @persona:Developer
Scenario: A folder in a {project.lower}'s {checkout.lower} is known as that {project.lower}'s, and any other folder is not
  Given a registered {project.lower} with a second {checkout.lower}, and a folder that is in no {project.lower}
  When the developer asks whether each of that {checkout.lower} and the other folder is a {project.lower}'s
  Then that {checkout.lower} is answered with the {project.lower}'s name and success
  And the other folder is answered with failure and nothing printed
```
