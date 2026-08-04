
.PHONY: ping init deploy force-restart

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