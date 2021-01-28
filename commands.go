package main

import (
	"os"

	"github.com/rivo/tview"
)

// RunCommand executes a command function based on the input string
func RunCommand(ui *ChatUI, command string) {
	switch command {
	case "/form":
		formCommand(ui.app)
	case "/exit":
		exitCommand(ui.app)
	default:
		ui.displaySelfMessage("Unknown command: " + command)
	}

}

func formCommand(app *tview.Application) {
	form := tview.NewForm().
		AddDropDown("Title", []string{"Mr.", "Ms.", "Mrs.", "Dr.", "Prof."}, 0, nil).
		AddInputField("First name", "", 20, nil, nil).
		AddInputField("Last name", "", 20, nil, nil).
		AddCheckbox("Age 18+", false, nil).
		AddPasswordField("Password", "", 10, '*', nil).
		AddButton("Save", nil).
		AddButton("Quit", func() {
			app.Stop()
		})
	form.SetBorder(true).SetTitle("Enter some data").SetTitleAlign(tview.AlignLeft)
	if err := app.SetRoot(form, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}

func exitCommand(app *tview.Application) {
	f := app.GetFocus()
	modal := tview.NewModal().
		SetText("Do you want to quit?").
		AddButtons([]string{"Quit", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonLabel {
			case "Quit":
				app.Stop()
				os.Exit(0)
			case "Cancel":
				app.SetRoot(f, true)
			}

		})
	if err := app.SetRoot(modal, false).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
