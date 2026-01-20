func VerifyNodesHealth(nodes []string) error {
	for _, node := range nodes {
		resp, err := http.Get(node + "/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			return fmt.Errorf("node %s is not healthy: %v", node, err)
		}
	}
	return nil
}

func WaitForNodesToBeHealthy(nodes []string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		if err := VerifyNodesHealth(nodes); err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for nodes to be healthy")
}