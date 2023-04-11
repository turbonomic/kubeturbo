#!/usr/bin/env python

import os
import shutil
from github import Github
import git
import re
import docker
import yaml
import getpass



def read_config_from_yaml(file_path):
    with open(file_path, 'r') as f:
        config = yaml.safe_load(f)
        return config

config_file_path = 'deploy/kubeturbo-operator/certified_operator_automation_config.yaml'
config = read_config_from_yaml(config_file_path)

local_repo_path = config.get('LOCAL_REPO_PATH')
operator_name = config.get('OPERATOR_NAME')
operator_release_version = config.get('OPERATOR_RELEASE_VERSION')
operator_dir_to_copy = config.get('OPERATOR_DIR_COPY')
operator_replace_version = config.get('OPERATOR_REPLACE_VERSION')
access_token = config.get('ACCESS_TOKEN')

#Enter the local repo path to clone repository to modify files
if local_repo_path is None:
    local_repo_path = input('Enter local repository path: ')

if operator_name is None:
   operator_name = input('Enter the name of certified operator to release (kubeturbo or prometurbo): ')

if operator_release_version is None:
   operator_release_version = input(f'Enter the version of the {operator_name} certified operator to release: ')

if operator_replace_version is None:
   operator_replace_version = input(f'Enter version of the {operator_name} operator to replace: ')

if operator_dir_to_copy is None:
   operator_dir_to_copy = input(f'Enter the {operator_name} operator version to copy from: ')

if access_token is None:
    access_token = getpass.getpass('Enter turbodeploy access token: ')

#icr repo parameters
icr_url = 'icr.io'
icr_namespace = 'cpopen'

#PR parameters
base_repo_name = 'certified-operators'
base_repo_owner = 'redhat-openshift-ecosystem'
origin_repo_name = 'certified-operators'
origin_repo_owner = 'turbodeploy'

#application parameters
kubeturbo_operator ='kubeturbo-operator'
prometurbo_operator ='prometurbo-operator'
beta_release = 'beta'
stable_release = 'stable'
certified_csv_yaml = 'certified.clusterserviceversion.yaml'
certified_annotations_yaml = 'annotations.yaml'

#regex patterns
operator_release_version_pattern = r'^\d+\.\d+\.\d+(-beta\.\d+)?$'
kubeturbo_operator_image_digest_pattern = r'(icr.io/cpopen/kubeturbo-operator@sha256:[a-f0-9]{64})'
prometurbo_operator_image_digest_pattern = r'(icr.io/cpopen/prometurbo-operator@sha256:[a-f0-9]{64})'
operator_version_for_beta_pattern = r'beta'
operator_version_remove_beta_pattern = r'^(\d+\.\d+\.\d+)-beta\.\d+$'
operator_to_substitute_image_key_pattern = r'(?<=image:\s)[^\s]+'
operator_to_substitue_replace_version_key_pattern = r'(?<=replaces:\s)[^\s]+'
operator_to_update_release_channel_pattern = r'(?<=operators\.operatorframework\.io\.bundle\.channels\.v1:\s)[^\s]+'

##To keep the code always in sync with base repository of redhat-openshift-ecosystem/certified-operators
#before releasing any new operator
def sync_fork_with_base_repo():
 repo_url = f'https://turbodeploy:{access_token}@github.com/turbodeploy/certified-operators.git'
# Clone the repository to a local directory
 git.Repo.clone_from(repo_url, local_repo_path)
# Add the base repository as a remote
 repo = git.Repo(local_repo_path)
 base_url = f'https://github.com/{base_repo_owner}/{base_repo_name}.git'
 repo.create_remote('base', base_url)
# Fetch the changes from the base repository
 base = repo.remote(name='base')
 base.fetch()
# Merge the changes into the local main branch
 repo.git.merge('base/main')
# Push the changes to the forked repository
 origin = repo.remote(name='origin')
 origin.push()

# Create Operator version folder and copy files into it from specified release version
def create_certified_operator_release_files():
 operator_path = f'/operators/{operator_name}-certified'
 operator_dir_path = local_repo_path + operator_path
 if not re.match(operator_release_version_pattern, operator_release_version):
        raise ValueError('Invalid operator version format. It allows the format of X.Y.Z,or X.Y.Z-beta.N where X, Y, Zand N are digits.')
 os.chdir(operator_dir_path)
 if os.path.exists(operator_release_version):
    raise ValueError('The version entered already exists, cannot release a duplicate version.')    
 os.mkdir(operator_release_version)
 print(f'{operator_name} operator version {operator_release_version} directory created successfully.')
# Copy files form user input operator version into new operator version folder
 for filename in os.listdir(operator_dir_to_copy):
    source_path = os.path.join(operator_dir_to_copy, filename)
    if os.path.isdir(source_path):
        shutil.copytree(source_path, os.path.join(operator_release_version, filename))
    else:
        shutil.copy(source_path, operator_release_version)
 print(f'{operator_name} operator version {operator_dir_to_copy} files copied successfully into {operator_release_version}.')

#To get regex pattern for the digest key based on operator
#'icr.io/cpopen/kubeturbo(or prometurbo)-operator@sha256: xxxxx' 
def get_operator_image_digest():
 client = docker.from_env()
 if operator_name == 'kubeturbo':
  digest_regex = kubeturbo_operator_image_digest_pattern
  icr_repo = kubeturbo_operator
 elif operator_name == 'prometurbo':
  digest_regex = prometurbo_operator_image_digest_pattern
  icr_repo = prometurbo_operator
#If other then kubeturbo or prometurbo    
 else:
    raise ValueError(f'{operator_name} digest entered is invalid.')
#To get icr tag version based on the operator release version
#strip of the part from beta in the version to query icr
 if re.search(operator_version_for_beta_pattern, operator_release_version):
  match = re.match(operator_version_remove_beta_pattern, operator_release_version)
  icr_tag=match.group(1)
 else:
  icr_tag = operator_release_version
# Pull ICR image to replace the digest
 image_name = f'{icr_url}/{icr_namespace}/{icr_repo}:{icr_tag}'
 image = client.images.pull(image_name)
 image_id = image.attrs['RepoDigests'][0]
 match = re.search(digest_regex, image_id)
 if match:
  digest = match.group(1)
 else:
  raise ValueError(f'No matching digest key found for {operator_name} operator.')
 return digest  

# Update the image digest key pulled from icr into operator csv
def update_image_digest(digest):
# Replace image digest key
 yaml_file_path = os.path.join(f'{operator_release_version}/manifests', f'{operator_name}-{certified_csv_yaml}')
 with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()
# Matches the string literal "image:" followed by a white space character to replace with new digest key
 yaml_str = re.sub(operator_to_substitute_image_key_pattern, digest, yaml_str)
 with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)
 print(f'{operator_name}-{certified_csv_yaml} image digest updated.')

# Update the operator versions and replace older version into operator csv
def update_operator_versions():
  yaml_file_path = os.path.join(f'{operator_release_version}/manifests', f'{operator_name}-{certified_csv_yaml}')
  operator_version_pattern = re.compile(operator_dir_to_copy)
  with open(yaml_file_path, 'r') as f:
    yaml_str = f.read()
#check for the version of the operator directory, it was copied from and change to new operator version entered by user
# Ex - If the operator version user entered is 8.8.4 and it was copied from 8.8.3, this will search for 8.8.3 version in the file
# to replace with 8.8.4
  if re.search(operator_version_pattern, yaml_str):
    new_version = re.sub(operator_version_pattern, operator_release_version, yaml_str)
    with open(yaml_file_path, 'w') as f:
        f.write(new_version)
  else:
    raise ValueError(f'{operator_name} operator version to change is not found in file.')
#replace the old operator version
  with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()
#matches the string literal "replaces:" followed by a white space character to replace with the version user entered
  yaml_str = re.sub(operator_to_substitue_replace_version_key_pattern, f'{operator_name}-operator.v{operator_replace_version}', yaml_str)
  with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)
  print(f'{operator_name}-{certified_csv_yaml} versions updated.')


# Updating annotations. yaml for stable or beta release
def update_release_channel():
 yaml_file_path = os.path.join(f'{operator_release_version}/metadata', certified_annotations_yaml)
 with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()
 if re.search(operator_version_for_beta_pattern, operator_release_version):
   yaml_str = re.sub(operator_to_update_release_channel_pattern, beta_release, yaml_str)
 else:
    yaml_str = re.sub(operator_to_update_release_channel_pattern, stable_release, yaml_str)
 with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)
 print(f'{operator_name}-{certified_annotations_yaml} file updated for channel release.')


# Commit and push changes to the turbodeploy repository
def commit_and_push_changes():
 repo = git.Repo(local_repo_path)
 repo.git.checkout('-b', f'{operator_name}-certified-{operator_release_version}')
 repo.git.add('.')
 repo.git.commit(m=f'{operator_name} certified operator {operator_release_version}')
 repo.git.push('-u', 'origin', f'{operator_name}-certified-{operator_release_version}')


# Create the pull request from turbodeploy:certified-operators to redhat-openshift-ecosystem:certified-operators
def create_pull_request():
 git_turbo_login = Github(access_token)
 branch_name = f'{operator_name}-certified-{operator_release_version}'

 base_repo = git_turbo_login.get_repo(f'{base_repo_owner}/{base_repo_name}')
 origin_repo = git_turbo_login.get_repo(f'{origin_repo_owner}/{origin_repo_name}')
 base_branch = base_repo.get_branch(base_repo.default_branch)
 head_branch = origin_repo.get_branch(branch_name)

 title = f'operator {operator_name}-certified ({operator_release_version})'
 body = f'New {operator_name} operator bundle release'
 pull_request = base_repo.create_pull(title=title,
                                     body=body,
                                     head=f'{origin_repo_owner}:{head_branch.name}',
                                     base=base_branch.name)

 print(f'Pull request created successfully: {pull_request.html_url}')


#Steps to release certified operator bundle into redhat openshift
# This will update the local repo copy with updated code from base repository and sync the forked repository
#if it's not upto date
sync_fork_with_base_repo()
#This will create a new operator version directory with the files (csv, crd, annotations) copied from user
#input release version
create_certified_operator_release_files()
#This will return the digest key from icr repo based on specified operator from user
#'icr.io/cpopen/kubeturbo(or prometurbo)-operator@sha256: xxxxx' 
digest_key = get_operator_image_digest()
#This will update the image digest pulled from get_operator_image_digest() function
#to update the csv with new one
update_image_digest(digest_key)
#This will update the versions in csv to get released and replaces the older version of the operator
#specified from user
update_operator_versions()
#This will update the release channel, if 'stable' or 'beta' based on the release version of user input
#like if version to release has beta(8.x.x-beta.x) this goes to beta channel, if no 'beta' this goes to
#stable channel release
update_release_channel()
#This will commit and push changes to the forked repository(turbodeploy/certified-operators) as a turbodeploy user
commit_and_push_changes()
#This will open a PR from forked repository(turbodeploy:certified-operators) to base repo(redhat-openshift-ecosystem:certified-operators)
create_pull_request()

