
.PHONY: ping init deploy force-restart

ping:
	ansible all -m ping

init:
	ansible-playbook playbooks/init.yml

deploy:
	ansible-playbook playbooks/deploy-antsd.yml

force-restart:
	ansible-playbook playbooks/deploy-antsd.yml --tags force-restart