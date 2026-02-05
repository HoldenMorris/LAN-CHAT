A local WIFI chat
===



To master Bubble Tea, you'll want to look at the bubbles repository and the lipgloss documentation. The "1 item" text you're seeing is part of the list component's "status bar."

Where to Learn
Charmbracelet Bubbles Repo: This is the source code for the List, Filepicker, and Progress bar. The list/list.go file is actually very readable and shows all the available properties.

Charmbracelet Lip Gloss Repo: This is where you learn how to handle layouts, colors, and borders.

Charm.sh Examples: The best way to learn is by reading their "Simple," "Real-time," and "Viewports" examples.

How to change the "1 item" text
The list component uses a field called Styles to control how things are rendered. To change the "1 item" or "X items" text, you actually need to provide a custom StatusMessage or customize the list.DefaultDelegate.

However, if you just want to change the wording of that status bar (like changing "items" to "peers"), you can modify the list.Model properties.

1. Changing the Label
The "1 item" text is dynamic. If you want to replace that whole status line with something custom, you can use:

Go
l.SetShowStatusBar(false) // Hide the default "1 item" bar
Then, in your View() function, you can manually add your own string like fmt.Sprintf("%d Peers Online", len(m.list.Items())).

2. Customizing the Styles
If you want to keep the bar but change the color or padding, you access the Styles sub-struct:

Go
l.Styles.StatusBar = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
3. Deep Customization (The Delegate)
The "1 item" text is often tied to the Delegate. When you created the list with list.NewDefaultDelegate(), you used a pre-built template. To change how items look (like adding timestamps or different colors to the titles), you can modify the delegate:

Go
d := list.NewDefaultDelegate()
d.ShowDescription = true
d.Styles.SelectedTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
l := list.New(items, d, width, height)
Pro Tip for "Full Control"
If you find the list bubble too "opinionated" (it does a lot of things automatically that are hard to undo), many developers eventually build their own custom list using a simple for loop in the View() function and a viewport bubble for scrolling. This gives you 100% control over every character on the screen.

Would you like me to show you how to create a "Compact" view mode that removes the status bar and help menu to give more space to the chat window?
