import os
import shutil
from github import Github
import git
import re
import docker

#icr parameters
icr_url = 'icr.io'
icr_namespace = 'cpopen'
#Enter the local repo path to clone repository to modify files
local_repo_path = os.getenv('LOCAL_REPO_PATH')
if local_repo_path is None:
    local_repo_path = input('Enter repository local path: ')
#PR parameters
base_repo_name = 'certified-operators'
base_repo_owner = 'redhat-openshift-ecosystem'
origin_repo_name = 'certified-operators'
origin_repo_owner = 'turbodeploy'
#Git access token
access_token = os.getenv('ACCESS_TOKEN')
if access_token is None:
    access_token = input('Enter turbodeploy access token: ')
repo_url = f'https://turbodeploy:{access_token}@github.com/turbodeploy/certified-operators.git'

# Clone the repository to a local directory
git.Repo.clone_from(repo_url, local_repo_path)

#To keep the code always in sync with base repository of redhat-openshift-ecosystem/certified-operators
#before releasing any new operator
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

# Create Operator version folder
operator_name = input('Enter the name of certified operator to release (kubeturbo or prometurbo):')
operator_path = f'/operators/{operator_name}-certified'
operator_dir_path = local_repo_path + operator_path
operator_release_version = input(f'Enter the version of the {operator_name} certified operator to release: ')
if not re.match(r'^\d+\.\d+\.\d+(-beta\.\d+)?$', operator_release_version):
        raise ValueError('Invalid operator version format. It allows the format of X.Y.Z,or X.Y.Z-beta.N where X, Y, Zand N are digits.')
os.chdir(operator_dir_path)
if os.path.exists(operator_release_version):
    raise ValueError('The version entered already exists, cannot release a duplicate version')    
os.mkdir(operator_release_version)
print(f'{operator_name} operator version {operator_release_version} directory created successfully.')

# Copy files form user input operator version into new operator version folder
operator_dir_to_copy = input(f'Enter the {operator_name} operator version to copy from: ')
for filename in os.listdir(operator_dir_to_copy):
    source_path = os.path.join(operator_dir_to_copy, filename)
    if os.path.isdir(source_path):
        shutil.copytree(source_path, os.path.join(operator_release_version, filename))
    else:
        shutil.copy(source_path, operator_release_version)
print(f'{operator_name} operator version {operator_dir_to_copy} files copied successfully into {operator_release_version}.')


client = docker.from_env()
#To get regex pattern for the digest key based on operator
#'icr.io/cpopen/kubeturbo(or prometurbo)-operator@sha256: xxxxx' 
if operator_name == 'kubeturbo':
 digest_regex = r'(icr.io/cpopen/kubeturbo-operator@sha256:[a-f0-9]{64})'
 icr_repo = 'kubeturbo-operator'
elif operator_name == 'prometurbo':
 digest_regex = r'(icr.io/cpopen/prometurbo-operator@sha256:[a-f0-9]{64})'
 icr_repo = 'prometurbo-operator'
#If other then kubeturbo or prometurbo    
else:
    raise ValueError(f'{operator_name} digest entered is invalid')

#To get icr tag version based on the operator release version
#strip of the part from beta in the version to query icr
if re.search(r'beta', operator_release_version):
 version_regex = r'^(\d+\.\d+\.\d+)-beta\.\d+$'
 match = re.match(version_regex, operator_release_version)
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
 raise ValueError(f'No matching digest key found for {operator_name} operator')    

# Updating csv
# Replace image digest key
yaml_file_path = os.path.join(f'{operator_release_version}/manifests', f'{operator_name}-certified.clusterserviceversion.yaml')
with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()
#matches the string literal "image:" followed by a white space character to replace with new digest key
yaml_str = re.sub(r'(?<=image:\s)[^\s]+', digest, yaml_str)
with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)

# Replace the operator version
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
replace_version = input(f'Enter version of the {operator_name} operator to replace: ')
with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()
#matches the string literal "replaces:" followed by a white space character to replace with the version user entered
yaml_str = re.sub(r'(?<=replaces:\s)[^\s]+', f'{operator_name}-operator.v{replace_version}', yaml_str)
with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)
print(f'{operator_name}-certified.clusterserviceversion.yaml file updated.')


# Updating annotations. yaml for stable or beta release
yaml_file_path = os.path.join(f'{operator_release_version}/metadata', 'annotations.yaml')

with open(yaml_file_path, 'r') as f:
   yaml_str = f.read()

if re.search(r'beta', operator_release_version):
   yaml_str = re.sub(r'(?<=operators\.operatorframework\.io\.bundle\.channels\.v1:\s)[^\s]+', 'beta', yaml_str)
else:
    yaml_str = re.sub(r'(?<=operators\.operatorframework\.io\.bundle\.channels\.v1:\s)[^\s]+', 'stable', yaml_str)

with open(yaml_file_path, 'w') as f:
    f.write(yaml_str)

print(f'{operator_name}-annotations.yaml file updated.')


# Commit and push changes to the turbodeploy repository
repo = git.Repo(local_repo_path)
repo.git.checkout('-b', f'{operator_name}-certified-{operator_release_version}')
repo.git.add('.')
repo.git.commit(m=f'{operator_name} certified operator {operator_release_version}')
repo.git.push('-u', 'origin', f'{operator_name}-certified-{operator_release_version}')


# Create the pull request from turbodeploy:certified-operators to redhat-openshift-ecosystem:certified-operators
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

