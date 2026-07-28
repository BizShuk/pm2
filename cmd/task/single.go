package task

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/bizshuk/pm2/process"
)

func chooseSingleApp(apps []process.AppConfig, in io.Reader, out io.Writer) (process.AppConfig, error) {
	if len(apps) == 0 {
		return selectSingleApp(apps, "")
	}

	fmt.Fprintln(out, "Apps in ecosystem file:")
	for i, app := range apps {
		fmt.Fprintf(out, "  %d) %s\n", i+1, appSelectionKey(app))
	}

	if len(apps) == 1 {
		fmt.Fprintln(out, "Applying the only app.")
		return selectSingleApp(apps, "1")
	}

	fmt.Fprint(out, "Choose one app (number or name): ")
	choice, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return process.AppConfig{}, fmt.Errorf("read single app selection: %w", err)
	}
	return selectSingleApp(apps, choice)
}
