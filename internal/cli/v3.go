package cli

import (
	"io"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

func newV3Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "v3",
		Short: "Read active tasks through the TickTick web API",
	}
	activeCmd := &cobra.Command{
		Use:   "active",
		Short: "Fetch and cache all active tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cacheIDs, err := cacheIDsRequested(cmd)
			if err != nil {
				return err
			}
			client, output, err := newV2CLIClient()
			if err != nil {
				return err
			}
			batch, err := client.BatchCheck(cmd.Context(), 0)
			if err != nil {
				return err
			}
			// Updates are authoritative when a task appears in both collections.
			tasks := make([]api.V3BatchTask, 0, len(batch.SyncTaskBean.Add)+len(batch.SyncTaskBean.Update))
			index := make(map[string]int, cap(tasks))
			for _, group := range [][]api.V3BatchTask{batch.SyncTaskBean.Add, batch.SyncTaskBean.Update} {
				for _, task := range group {
					if i, exists := index[task.ID]; exists {
						tasks[i] = task
					} else {
						index[task.ID] = len(tasks)
						tasks = append(tasks, task)
					}
				}
			}
			active := make([]api.V2Task, 0, len(tasks))
			for _, task := range tasks {
				if task.Status == 0 && task.Deleted == 0 {
					active = append(active, task.V2Task)
				}
			}
			if err := cacheWebTasks(cmd.Context(), active); err != nil {
				return err
			}
			safe := output.cachedTasks(active, cacheIDs)
			return v2Render(safe, func(w io.Writer) {
				v2PrintTasks(w, safe)
			})
		},
	}
	addCacheIDsFlag(activeCmd)
	cmd.AddCommand(activeCmd)
	return cmd
}

func init() {
	rootCmd.AddCommand(newV3Cmd())
}
