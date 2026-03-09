export interface FileEntry {
	name: string;
	path: string;
	isDirectory: boolean;
	size?: number;
	modifiedAt?: string;
}

export interface FileNode {
	name: string;
	path: string;
	isDirectory: boolean;
	children?: FileNode[];
	size?: number;
	modifiedAt?: string;
}

