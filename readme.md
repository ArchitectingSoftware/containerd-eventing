## Containerd Events Prototype

Needed to update another tool that I am working on to monitor for containerd events.  I have found containerd to be pretty awesome, but programming against it has been challenging given the lack of documentation on how to use its various APIs.  I had to look at a lot of the core code as well as code used to implement containerd tools including `crictl` and `nerdctl`.  Posting this code here that I extracted from the version I am using in my other project in case others find it useful.

Enjoy, and please post suggestions for improvement.  I still find how I had to query for the process ids that run in a container a little awkward. One of the main challenges in implementing this was filtering out sandbox containers from the containers I was interested in monitoring.

The basic idea for this proof-of-concept was to connect to containerd, get a list of all running containers, and then watch for containerd events to update my internal list that I am tracking.  
