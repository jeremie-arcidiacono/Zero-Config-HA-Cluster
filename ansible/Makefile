
.PHONY: help ping shutdown init deploy force-restart wipe logs logs-kill hello-world hello-world-check hello-world-delete

help:
	@echo "Available commands:"
	@echo "  ping               - Ping all hosts"
	@echo "  shutdown           - Shutdown all hosts"
	@echo "  init               - Run initialization playbook"
	@echo "  deploy             - Deploy ants-daemon"
	@echo "  force-restart      - Force restart ants-daemon"
	@echo "  wipe               - Uninstall k3s and reset antsd state on all hosts"
	@echo "  logs               - Tail antsd logs of all active nodes in one tmux window"
	@echo "  logs-kill          - Kill the antsd-logs tmux session"
	@echo "  hello-world        - Deploy the hello-world test app on k3s and verify it"
	@echo "  hello-world-check  - Re-verify the hello-world app"
	@echo "  hello-world-delete - Remove the hello-world test app"

ping:
	ansible all -m ping

shutdown:
	ansible all -b -B 30 -P 0 -m shell -a "sleep 2 && systemctl poweroff"

init:
	ansible-playbook playbooks/init.yml

deploy:
	ansible-playbook playbooks/deploy-antsd.yml

force-restart:
	ansible-playbook playbooks/deploy-antsd.yml --tags force-restart

wipe:
	ansible-playbook playbooks/wipe.yml

logs:
	@bash scripts/tail-logs.sh

logs-kill:
	-tmux kill-session -t antsd-logs

hello-world:
	ansible-playbook playbooks/hello-world.yml

hello-world-check:
	ansible-playbook playbooks/hello-world.yml --tags check

hello-world-delete:
	ansible-playbook playbooks/hello-world.yml -e hello_world_state=absent