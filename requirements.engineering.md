# ArchIO Requirement Enginnering 
As a service that's provide an object storage server, self deployed
## Actors
- Root: a user that has the full permissions over users permissions.
- Customer: a user that creates an account throught the singup API.
- Workers: a system users that has the permessions to put documents in archive if the meet some conditions

## Storage Diffusions:
- Container: every customer have one or more container, buckets are used to specify the region of storage.
- Folder: every container can have multiple folders.
- Object: is the elements the customer is going to store, once the object are pushed to archIO an URI is generated to each object.
## Notes: 
- Container: In a global vision container name is unique across all users, containers has expose permissions, even in a no public container, container's objects could be public.
- Object: should have a max size.
